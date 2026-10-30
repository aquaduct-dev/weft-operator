/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"aquaduct.dev/weft-operator/internal/resource"
	"context"
	"flag"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
	"aquaduct.dev/weft-operator/internal/controller/dnsrecord"
	"aquaduct.dev/weft-operator/internal/controller/weftgateway"
	"aquaduct.dev/weft-operator/internal/controller/weftlocaldnsreflector"
	"aquaduct.dev/weft-operator/internal/controller/weftserver"
	"aquaduct.dev/weft-operator/internal/controller/wefttunnel"
	"aquaduct.dev/weft-operator/internal/nodeagent"
	"aquaduct.dev/weft-operator/internal/trust"
	"aquaduct.dev/weft-operator/internal/webhook/trustinjector"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	//+kubebuilder:scaffold:imports
)

// webhookCertDir is where the trust webhook's serving certificate is written
// (an emptyDir in the chart; the root filesystem is read-only).
const webhookCertDir = "/tmp/k8s-webhook-server/serving-certs"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(weftv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.AddToScheme(scheme))
	// v1alpha2 is needed because the weftgateway reconciler watches TCPRoute,
	// TLSRoute, and UDPRoute — Gateway API's experimental channel kinds.
	utilruntime.Must(gatewayv1alpha2.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	// The same binary is the WeftLocalDNSReflector node agent, so the
	// DaemonSet needs no second image.
	if len(os.Args) > 1 && os.Args[1] == "node-agent" {
		os.Exit(nodeagent.Run(os.Args[2:]))
	}

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "weft-operator.aquaduct.dev",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Cache: cache.Options{ByObject: reflectorOwnedOnly()},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			CertDir: webhookCertDir,
		}),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		os.Exit(1)
	}

	// Preflight: the Gateway API CRDs are no longer bundled with this
	// chart, so the cluster operator must install them out-of-band before
	// weft-operator starts. Failing here with a clear message is much
	// nicer than the cryptic informer-cache error you'd get from
	// mgr.Start when the GVK is unknown to the REST mapper.
	gatewayGV := gatewayv1.GroupVersion.String()
	if _, err := clientset.Discovery().ServerResourcesForGroupVersion(gatewayGV); err != nil {
		if apierrors.IsNotFound(err) {
			setupLog.Error(err, "Gateway API CRDs are not installed in this cluster — install them before deploying weft-operator (see https://gateway-api.sigs.k8s.io/guides/)",
				"groupVersion", gatewayGV)
			os.Exit(1)
		}
		setupLog.Error(err, "failed to query Gateway API CRDs", "groupVersion", gatewayGV)
		os.Exit(1)
	}

	// WEFT_IMAGE runs everything the operator starts from weft: tunnel
	// clients, servers, node probes and reflector proxies.
	weftImage := envOr("WEFT_IMAGE", resource.DefaultWeftImage)
	if err := (&weftserver.WeftServerReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Clientset: clientset,
		WeftImage: weftImage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WeftServer")
		os.Exit(1)
	}
	if err := (&wefttunnel.WeftTunnelReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		WeftImage: weftImage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WeftTunnel")
		os.Exit(1)
	}
	// AUTHZ_ENDPOINT overrides where the long-lived access token is exchanged
	// for a short-lived JWT (default https://authz.aquaduct.dev).
	taasAPIClient := aquaducttaas.NewHTTPAPIClient("", os.Getenv("AUTHZ_ENDPOINT"))
	if err := (&aquaducttaas.AquaductTaaSReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		APIClient: taasAPIClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AquaductTaaS")
		os.Exit(1)
	}
	if err := (&dnsrecord.DNSRecordReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		APIClient: taasAPIClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSRecord")
		os.Exit(1)
	}
	missingRoutes, err := missingRouteKinds(clientset.Discovery())
	if err != nil {
		setupLog.Error(err, "failed to query Gateway API route CRDs")
		os.Exit(1)
	}
	for kind := range missingRoutes {
		setupLog.Info("Gateway API CRD not installed; its routes are ignored until the operator restarts", "kind", kind,
			"groupVersion", gatewayv1alpha2.GroupVersion.String())
	}
	if err := (&weftgateway.WeftGatewayReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		MissingRouteKinds: missingRoutes,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WeftGateway")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	// Trust injection: every new pod gets the WeftLocalDNSReflector CAs (see
	// internal/trust). The operator's own namespace and kube-system are
	// excluded so the webhook can never get in the way of the operator or
	// the control plane coming up.
	operatorNamespace := envOr("POD_NAMESPACE", "weft-system")
	webhookCfg := trustinjector.Config{
		Namespace:          operatorNamespace,
		ServiceName:        envOr("WEBHOOK_SERVICE_NAME", "weft-operator-webhook"),
		CertDir:            webhookCertDir,
		ExcludedNamespaces: []string{"kube-system", operatorNamespace},
	}
	setupClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create setup client")
		os.Exit(1)
	}
	if err := trustinjector.Setup(ctrl.LoggerInto(context.Background(), setupLog), setupClient, webhookCfg); err != nil {
		setupLog.Error(err, "unable to set up trust injection webhook")
		os.Exit(1)
	}
	if err := mgr.Add(&trustinjector.Renewer{Client: setupClient, Config: webhookCfg}); err != nil {
		setupLog.Error(err, "unable to add webhook certificate renewer")
		os.Exit(1)
	}
	trustSource := &trust.Source{Reader: mgr.GetClient(), SystemRootsFile: envOr("SYSTEM_ROOTS_FILE", trust.DefaultSystemRootsFile)}
	mgr.GetWebhookServer().Register(trustinjector.Path, &webhook.Admission{Handler: &trustinjector.Handler{
		Client:  mgr.GetClient(),
		Source:  trustSource,
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}})

	if err := (&weftlocaldnsreflector.Reconciler{
		Client:             mgr.GetClient(),
		APIReader:          mgr.GetAPIReader(),
		Scheme:             mgr.GetScheme(),
		WeftImage:          weftImage,
		OperatorImage:      os.Getenv("WEFT_OPERATOR_IMAGE"),
		Trust:              trustSource,
		ExcludedNamespaces: webhookCfg.ExcludedNamespaces,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WeftLocalDNSReflector")
		os.Exit(1)
	}

	// The cluster-default reflector (chart: defaultReflector.*).
	if err := mgr.Add(&weftlocaldnsreflector.DefaultReflector{
		Client:    mgr.GetClient(),
		Enabled:   envOr("DEFAULT_REFLECTOR_ENABLED", "true") == "true",
		Name:      envOr("DEFAULT_REFLECTOR_NAME", "cluster-default"),
		Hostnames: splitList(os.Getenv("DEFAULT_REFLECTOR_HOSTNAMES")),
	}); err != nil {
		setupLog.Error(err, "unable to add default reflector")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// reflectorOwnedOnly limits the manager's ConfigMap informer to the trust
// bundle ConfigMaps a WeftLocalDNSReflector publishes. Nothing else in the operator reads
// ConfigMaps, and without it the reflector's first Get would cache every
// ConfigMap in the cluster (dashboards, bundles, ...), a real memory risk for
// an operator that has already been OOM-killed. The shared CoreDNS ConfigMap
// is read through the API reader instead. Secrets are deliberately NOT
// filtered: the DNSRecord and AquaductTaaS reconcilers read unlabelled
// access-token Secrets through the cache.
func reflectorOwnedOnly() map[client.Object]cache.ByObject {
	req, err := labels.NewRequirement(trust.ManagedLabel, selection.Equals, []string{trust.ManagedValue})
	if err != nil {
		panic(err)
	}
	sel := labels.NewSelector().Add(*req)
	return map[client.Object]cache.ByObject{
		&corev1.ConfigMap{}: {Label: sel},
	}
}

// missingRouteKinds returns the optional Gateway API route kinds the
// cluster does not serve. Only the experimental channel installs them, and
// a watch on an unserved kind would stop the manager (see
// weftgateway.WeftGatewayReconciler.MissingRouteKinds).
func missingRouteKinds(d discovery.DiscoveryInterface) (map[string]bool, error) {
	served := map[string]bool{}
	list, err := d.ServerResourcesForGroupVersion(gatewayv1alpha2.GroupVersion.String())
	switch {
	case apierrors.IsNotFound(err):
		// No v1alpha2 at all: every optional kind is missing.
	case err != nil:
		return nil, err
	default:
		for _, r := range list.APIResources {
			served[r.Kind] = true
		}
	}
	missing := map[string]bool{}
	for _, kind := range weftgateway.OptionalRouteKinds {
		if !served[kind] {
			missing[kind] = true
		}
	}
	return missing, nil
}

// splitList parses a comma-separated env value, dropping empty entries.
func splitList(v string) []string {
	var out []string
	for s := range strings.SplitSeq(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
