// scenario.go — the WeftLocalDNSReflector e2e scenario. Each step is one
// clusterharness pass condition; a failing step stops the run (later steps
// depend on it) and dumps diagnostics to the console.
//
// The reflected hostname, registry.weft-e2e.test, cannot resolve anywhere
// (.test is reserved and the VM has no network), so every successful request
// to it is proof that the in-cluster path works; the registry access log
// then shows that every request came from the reflector proxy.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/clusterharness"
	"aquaduct.dev/weft-operator/internal/pki"
)

const (
	hostname      = "registry.weft-e2e.test"
	reflectorName = "e2e"
	operatorNS    = "weft-system"
	backendNS     = "e2e-backend"
	// operatorManifest is the rendered chart and gatewayAPICRDs the CRDs
	// the operator requires, both baked into the rootfs.
	operatorManifest = "/etc/e2e/weft-operator.yaml"
	gatewayAPICRDs   = "/etc/e2e/gateway-api-crds.yaml"
	// Node paths the agent manages (the scenario runs on the node).
	nodeHostsFile = "/etc/hosts"
	nodeCertsDir  = "/var/lib/rancher/k3s/agent/etc/containerd/certs.d"
	// nodeCABundle is the node's system trust store (a real Mozilla bundle
	// baked into the rootfs); pristineCABundle is a copy taken before any
	// reflector exists, used for the control run and the teardown check.
	nodeCABundle     = "/etc/ssl/certs/ca-certificates.crt"
	pristineCABundle = "/tmp/pristine-ca-certificates.crt"
)

type scenario struct {
	kubeconfig string
	// refl is the reflector under test: "e2e" (created by the scenario) or
	// the operator's cluster-default one.
	refl      string
	c         client.Client
	clientset *kubernetes.Clientset

	// Filled in as the scenario progresses.
	serviceIP     string
	caFingerprint string
}

// caBlockMarker starts the node agent's managed block in the node bundle.
func (s *scenario) caBlockMarker() string {
	return "# BEGIN weft-local-dns-reflector " + s.refl + " CA"
}

func newScenario(kubeconfig string) (*scenario, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := weftv1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &scenario{kubeconfig: kubeconfig, refl: reflectorName, c: c, clientset: cs}, nil
}

type step struct {
	id, label string
	fn        func(context.Context) error
}

// reflectorSteps exercise an explicitly created reflector end to end.
func (s *scenario) reflectorSteps() []step {
	return []step{
		{"e2e.operator-ready", "operator installed from the chart and webhook registered", s.installOperator},
		{"e2e.backend-ready", "registry backend running", s.deployBackend},
		{"e2e.unresolvable-before", "hostname does not resolve without the reflector", s.checkUnresolvable},
		{"e2e.trust-gate-holds", "DNS withheld while a running pod lacks the CA", s.createReflectorAndCheckGate},
		{"e2e.trust-injected", "restarted pod gets the CA injected; reflector becomes Ready", s.restartBackendAndWaitReady},
		{"e2e.pod-path", "pod resolves to the proxy, verifies TLS with system roots, pushes an image", s.checkPodPath},
		{"e2e.node-path", "containerd pulls the pushed image through the reflector", s.checkNodePath},
		{"e2e.node-host-path", "a process on the node resolves the reflected name and accepts the proxy cert from the node trust store", s.checkNodeHostPath},
		{"e2e.traffic-internal", "every registry request arrived from the reflector proxy", s.checkTrafficInternal},
		{"e2e.teardown", "deleting the reflector removes DNS, node entries and the node trust store CA", s.checkTeardown},
	}
}

// run executes steps in order; a failed step stops the run (later steps
// depend on it) and dumps diagnostics.
func (s *scenario) run(ctx context.Context, steps []step) []clusterharness.ReportPC {
	var pcs []clusterharness.ReportPC
	failed := ""
	for _, st := range steps {
		pc := clusterharness.ReportPC{ID: st.id, Label: st.label}
		if failed != "" {
			pc.Error = "not run: " + failed + " failed"
			pcs = append(pcs, pc)
			continue
		}
		log.Printf("=== %s: %s", st.id, st.label)
		start := time.Now()
		if err := st.fn(ctx); err != nil {
			pc.Error = err.Error()
			failed = st.id
			log.Printf("--- FAIL %s (%s): %v", st.id, time.Since(start).Round(time.Second), err)
			s.dumpDiagnostics(context.Background())
		} else {
			pc.Passed = true
			log.Printf("--- PASS %s (%s)", st.id, time.Since(start).Round(time.Second))
		}
		pcs = append(pcs, pc)
	}
	return pcs
}

func (s *scenario) installOperator(ctx context.Context) error {
	// Snapshot the node trust store before any reflector exists.
	pristine, err := os.ReadFile(nodeCABundle)
	if err != nil {
		return fmt.Errorf("node has no system trust store: %w", err)
	}
	if strings.Contains(string(pristine), "weft-local-dns-reflector") {
		return fmt.Errorf("node trust store already contains a reflector CA")
	}
	if err := os.WriteFile(pristineCABundle, pristine, 0o644); err != nil {
		return err
	}
	log.Printf("node trust store: %s, %d certificates", nodeCABundle, pki.CountCerts(pristine))

	if err := waitFor(ctx, 3*time.Minute, "CoreDNS available", s.deploymentReady("kube-system", "coredns")); err != nil {
		return err
	}
	if err := s.kubectlApply(ctx, gatewayAPICRDs, "default"); err != nil {
		return err
	}
	if err := s.kubectlApply(ctx, operatorManifest, operatorNS); err != nil {
		return err
	}
	if err := waitFor(ctx, 3*time.Minute, "operator available", s.deploymentReady(operatorNS, "weft-operator")); err != nil {
		return err
	}
	return waitFor(ctx, time.Minute, "trust webhook registered", func() (bool, string, error) {
		mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
		err := s.c.Get(ctx, types.NamespacedName{Name: "weft-operator-trust-injector"}, mwc)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		return err == nil && len(mwc.Webhooks) == 1 && len(mwc.Webhooks[0].ClientConfig.CABundle) > 0, "present", err
	})
}

func (s *scenario) deployBackend(ctx context.Context) error {
	labels := map[string]string{"app": "registry"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: backendNS, Name: "registry"},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr(false),
					Containers: []corev1.Container{{
						Name:            "registry",
						Image:           "docker.io/library/registry:2.8.3",
						ImagePullPolicy: corev1.PullNever,
						// Relative Location headers: the registry sees plain
						// http from the proxy and must not send clients there.
						Env:   []corev1.EnvVar{{Name: "REGISTRY_HTTP_RELATIVEURLS", Value: "true"}},
						Ports: []corev1.ContainerPort{{ContainerPort: 5000}},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/v2/", Port: intstr.FromInt(5000)},
						}, PeriodSeconds: 2},
					}},
				},
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: backendNS, Name: "registry"},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    []corev1.ServicePort{{Port: 5000, TargetPort: intstr.FromInt(5000)}},
		},
	}
	for _, o := range []client.Object{dep, svc} {
		if err := s.c.Create(ctx, o); err != nil {
			return fmt.Errorf("create %T: %w", o, err)
		}
	}
	return waitFor(ctx, 3*time.Minute, "registry available", s.deploymentReady(backendNS, "registry"))
}

func (s *scenario) checkUnresolvable(ctx context.Context) error {
	_, err := s.runClientPod(ctx, "before", "--host="+hostname, "--expect-unresolvable")
	return err
}

func (s *scenario) createReflectorAndCheckGate(ctx context.Context) error {
	if err := s.createTunnel(ctx); err != nil {
		return err
	}
	refl := &weftv1alpha1.WeftLocalDNSReflector{
		ObjectMeta: metav1.ObjectMeta{Name: s.refl},
		Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{hostname}},
	}
	if err := s.c.Create(ctx, refl); err != nil {
		return fmt.Errorf("create reflector: %w", err)
	}

	// The registry pod predates the reflector, so it does not trust the
	// CA: the gate must hold DNS and the node agent back and name it.
	if err := waitFor(ctx, 3*time.Minute, "trust gate closed on the registry pod", func() (bool, string, error) {
		r, err := s.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		dns := meta.FindStatusCondition(r.Status.Conditions, "DNSPublished")
		state := fmt.Sprintf("untrusted=%d sample=%v dns=%v", r.Status.UntrustedPods, r.Status.UntrustedPodSample, condReason(dns))
		gated := dns != nil && dns.Reason == "WaitingForTrust" &&
			slices.ContainsFunc(r.Status.UntrustedPodSample, func(p string) bool { return strings.HasPrefix(p, backendNS+"/registry-") })
		return gated, state, nil
	}); err != nil {
		return err
	}
	if s.coreDNSHasOverride(ctx) {
		return fmt.Errorf("CoreDNS override published while an untrusted pod is running")
	}
	err := s.c.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: s.refl + "-node-agent"}, &appsv1.DaemonSet{})
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("node agent deployed while an untrusted pod is running (get err=%v)", err)
	}
	if bundle, _ := os.ReadFile(nodeCABundle); strings.Contains(string(bundle), s.caBlockMarker()) {
		return fmt.Errorf("node trust store changed while an untrusted pod is running")
	}
	return nil
}

// createTunnel publishes the registry as https://registry.weft-e2e.test the
// way a weft Gateway would: as a WeftTunnel (no WeftServer, so nothing ever
// leaves the VM).
func (s *scenario) createTunnel(ctx context.Context) error {
	return s.c.Create(ctx, &weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: backendNS, Name: "registry"},
		Spec: weftv1alpha1.WeftTunnelSpec{
			SrcURL: "http://registry." + backendNS + ".svc:5000",
			DstURL: "https://" + hostname + "/",
		},
	})
}

func (s *scenario) restartBackendAndWaitReady(ctx context.Context) error {
	secret := &corev1.Secret{}
	if err := s.c.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: s.refl + "-ca"}, secret); err != nil {
		return fmt.Errorf("get reflector CA: %w", err)
	}
	s.caFingerprint = pki.Fingerprint(secret.Data[corev1.TLSCertKey])

	if err := s.c.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(backendNS), client.MatchingLabels{"app": "registry"}); err != nil {
		return fmt.Errorf("restart registry: %w", err)
	}
	if err := waitFor(ctx, 3*time.Minute, "replacement registry pod running with the CA injected", func() (bool, string, error) {
		pods := &corev1.PodList{}
		if err := s.c.List(ctx, pods, client.InNamespace(backendNS), client.MatchingLabels{"app": "registry"}); err != nil {
			return false, "", err
		}
		for _, p := range pods.Items {
			if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
				continue
			}
			ann := p.Annotations["weft.aquaduct.dev/trusted-cas"]
			mounted := false
			for _, m := range p.Spec.Containers[0].VolumeMounts {
				mounted = mounted || m.MountPath == "/etc/ssl/certs/ca-certificates.crt"
			}
			return strings.Contains(ann, s.caFingerprint) && mounted, fmt.Sprintf("%s trusted-cas=%q mounted=%v", p.Name, ann, mounted), nil
		}
		return false, fmt.Sprintf("%d pods, none running", len(pods.Items)), nil
	}); err != nil {
		return err
	}

	return waitFor(ctx, 5*time.Minute, "reflector Ready", func() (bool, string, error) {
		r, err := s.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		s.serviceIP = r.Status.ServiceIP
		var parts []string
		for _, c := range r.Status.Conditions {
			parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
		}
		return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready"), strings.Join(parts, " "), nil
	})
}

func (s *scenario) checkPodPath(ctx context.Context) error {
	// CoreDNS reloads imported files on its own schedule (and the ConfigMap
	// volume lags), so retry until the override is live.
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		_, lastErr = s.runClientPod(ctx, "probe", "--host="+hostname, "--expect-ip="+s.serviceIP, "--push=e2e/self")
		if lastErr == nil {
			return nil
		}
		log.Printf("pod path attempt %d failed: %v; retrying", attempt, lastErr)
		time.Sleep(15 * time.Second)
	}
	return lastErr
}

func (s *scenario) checkNodePath(ctx context.Context) error {
	if err := waitFor(ctx, 3*time.Minute, "node agent wrote /etc/hosts and containerd trust", func() (bool, string, error) {
		hosts, _ := os.ReadFile(nodeHostsFile)
		_, terr := os.Stat(nodeCertsDir + "/" + hostname + "/hosts.toml")
		ok := strings.Contains(string(hosts), s.serviceIP+" "+hostname) && terr == nil
		return ok, fmt.Sprintf("hosts-entry=%v hosts.toml=%v", strings.Contains(string(hosts), hostname), terr == nil), nil
	}); err != nil {
		return err
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: clientNS, Name: "pulled"},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: ptr(false),
			Containers: []corev1.Container{{
				Name:            "pulled",
				Image:           hostname + "/e2e/self:1",
				ImagePullPolicy: corev1.PullAlways,
			}},
		},
	}
	if err := s.c.Create(ctx, pod); err != nil {
		return err
	}
	return waitFor(ctx, 4*time.Minute, "pod running from an image pulled via the reflector", func() (bool, string, error) {
		p := &corev1.Pod{}
		if err := s.c.Get(ctx, client.ObjectKeyFromObject(pod), p); err != nil {
			return false, "", err
		}
		return p.Status.Phase == corev1.PodRunning, string(p.Status.Phase) + " " + containerState(p), nil
	})
}

// checkNodeHostPath runs this binary's client mode as a plain process on the
// k3s node (a fresh process, so Go loads the system roots now, after the
// agent edited them). It resolves through the node's resolver (/etc/hosts),
// connects to the proxy over the node's network namespace, and must verify
// the proxy certificate using only the node trust store. A control run
// against the pristine bundle must fail with an unknown-authority error,
// proving the acceptance comes from the CA the node agent installed.
func (s *scenario) checkNodeHostPath(ctx context.Context) error {
	if err := waitFor(ctx, 3*time.Minute, "node agent added the reflector CA to the node trust store", func() (bool, string, error) {
		bundle, err := os.ReadFile(nodeCABundle)
		if err != nil {
			return false, "", err
		}
		pristine, _ := os.ReadFile(pristineCABundle)
		have := strings.Contains(string(bundle), s.caBlockMarker())
		kept := strings.HasPrefix(string(bundle), strings.TrimRight(string(pristine), "\n"))
		return have && kept, fmt.Sprintf("ca-block=%v original-roots-kept=%v certs=%d", have, kept, pki.CountCerts(bundle)), nil
	}); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	run := func(extraEnv []string, args ...string) error {
		cmd := exec.CommandContext(ctx, self, append([]string{"client", "--host=" + hostname, "--expect-ip=" + s.serviceIP}, args...)...)
		// A clean environment: no SSL_CERT_FILE/SSL_CERT_DIR overrides
		// unless the control run asks for one.
		cmd.Env = append([]string{"PATH=/usr/local/bin:/bin:/sbin"}, extraEnv...)
		out, err := cmd.CombinedOutput()
		log.Printf("node host client %v %v:\n%s", extraEnv, args, out)
		return err
	}
	if err := run(nil); err != nil {
		return fmt.Errorf("node process could not reach the reflected server with the node trust store: %w", err)
	}
	if err := run([]string{"SSL_CERT_FILE=" + pristineCABundle, "SSL_CERT_DIR=/nonexistent"}, "--expect-untrusted"); err != nil {
		return fmt.Errorf("control run without the reflector CA did not fail as expected: %w", err)
	}
	return nil
}

// accessLogPeer matches the TCP peer at the start of the registry's
// combined access log lines ("10.244.0.9 - - [..] "GET /v2/ ...").
var accessLogPeer = regexp.MustCompile(`(?m)^(\d+\.\d+\.\d+\.\d+) - - \[[^\]]*\] "(\w+) (\S+)`)

func (s *scenario) checkTrafficInternal(ctx context.Context) error {
	proxyIPs := map[string]bool{}
	pods := &corev1.PodList{}
	if err := s.c.List(ctx, pods, client.InNamespace(operatorNS), client.MatchingLabels{"weft.aquaduct.dev/reflector": s.refl, "app.kubernetes.io/component": "proxy"}); err != nil {
		return err
	}
	for _, p := range pods.Items {
		proxyIPs[p.Status.PodIP] = true
	}
	if len(proxyIPs) == 0 {
		return fmt.Errorf("no proxy pods found")
	}
	regPods := &corev1.PodList{}
	if err := s.c.List(ctx, regPods, client.InNamespace(backendNS), client.MatchingLabels{"app": "registry"}); err != nil {
		return err
	}
	requests, foreign := 0, []string{}
	sawPush := false
	for _, p := range regPods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		logs, err := s.podLogs(ctx, backendNS, p.Name)
		if err != nil {
			return fmt.Errorf("registry logs: %w", err)
		}
		for _, m := range accessLogPeer.FindAllStringSubmatch(logs, -1) {
			peer, method, path := m[1], m[2], m[3]
			if path == "/v2/" && method == "GET" && !proxyIPs[peer] {
				// kubelet readiness probes hit /v2/ directly; not client traffic.
				continue
			}
			requests++
			sawPush = sawPush || (method == "PUT" && strings.Contains(path, "/manifests/1"))
			if !proxyIPs[peer] {
				foreign = append(foreign, fmt.Sprintf("%s %s %s", peer, method, path))
			}
		}
	}
	log.Printf("registry saw %d client requests; proxy IPs %v; non-proxy peers %v", requests, keys(proxyIPs), foreign)
	switch {
	case requests == 0:
		return fmt.Errorf("registry logged no client requests")
	case !sawPush:
		return fmt.Errorf("registry never saw the manifest push")
	case len(foreign) > 0:
		return fmt.Errorf("requests reached the registry without going through the proxy: %v", foreign)
	}
	return nil
}

func (s *scenario) checkTeardown(ctx context.Context) error {
	r, err := s.reflector(ctx)
	if err != nil {
		return err
	}
	if err := s.c.Delete(ctx, r); err != nil {
		return err
	}
	return waitFor(ctx, 3*time.Minute, "reflector, CoreDNS override and node entries removed", func() (bool, string, error) {
		_, gerr := s.reflector(ctx)
		gone := apierrors.IsNotFound(gerr)
		dns := s.coreDNSHasOverride(ctx)
		hosts, _ := os.ReadFile(nodeHostsFile)
		_, terr := os.Stat(nodeCertsDir + "/" + hostname)
		hostsLeft := strings.Contains(string(hosts), hostname)
		certsLeft := terr == nil
		bundle, _ := os.ReadFile(nodeCABundle)
		pristine, _ := os.ReadFile(pristineCABundle)
		bundleRestored := string(bundle) == string(pristine)
		return gone && !dns && !hostsLeft && !certsLeft && bundleRestored,
			fmt.Sprintf("reflector-gone=%v dns=%v hosts=%v certs.d=%v trust-store-restored=%v", gone, dns, hostsLeft, certsLeft, bundleRestored), nil
	})
}

// --- helpers -----------------------------------------------------------------

func (s *scenario) reflector(ctx context.Context) (*weftv1alpha1.WeftLocalDNSReflector, error) {
	r := &weftv1alpha1.WeftLocalDNSReflector{}
	err := s.c.Get(ctx, types.NamespacedName{Name: s.refl}, r)
	return r, err
}

func (s *scenario) coreDNSHasOverride(ctx context.Context) bool {
	cm := &corev1.ConfigMap{}
	if err := s.c.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-custom"}, cm); err != nil {
		return false
	}
	_, ok := cm.Data["weft-local-dns-reflector-"+s.refl+".server"]
	return ok
}

func (s *scenario) deploymentReady(ns, name string) func() (bool, string, error) {
	return func() (bool, string, error) {
		d := &appsv1.Deployment{}
		err := s.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, d)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		if err != nil {
			return false, "", err
		}
		return d.Status.AvailableReplicas > 0, fmt.Sprintf("available=%d", d.Status.AvailableReplicas), nil
	}
}

// dumpDiagnostics prints what is needed to debug a failed step from the
// serial console alone.
func (s *scenario) dumpDiagnostics(ctx context.Context) {
	log.Printf("##### diagnostics")
	if r, err := s.reflector(ctx); err == nil {
		log.Printf("reflector status: serviceIP=%s routes=%v untrusted=%d %v", r.Status.ServiceIP, r.Status.Routes, r.Status.UntrustedPods, r.Status.UntrustedPodSample)
		for _, c := range r.Status.Conditions {
			log.Printf("  %s=%s %s: %s", c.Type, c.Status, c.Reason, c.Message)
		}
	}
	pods := &corev1.PodList{}
	if err := s.c.List(ctx, pods); err == nil {
		for _, p := range pods.Items {
			log.Printf("pod %s/%s %s ip=%s %s", p.Namespace, p.Name, p.Status.Phase, p.Status.PodIP, containerState(&p))
		}
	}
	for _, sel := range []struct{ ns, label string }{
		{operatorNS, "app.kubernetes.io/name=weft-operator"},
		{operatorNS, "app.kubernetes.io/component=proxy"},
		{operatorNS, "app.kubernetes.io/component=node-agent"},
		{"kube-system", "k8s-app=kube-dns"},
	} {
		pl, err := s.clientset.CoreV1().Pods(sel.ns).List(ctx, metav1.ListOptions{LabelSelector: sel.label})
		if err != nil {
			continue
		}
		for _, p := range pl.Items {
			logs, _ := s.podLogs(ctx, p.Namespace, p.Name)
			lines := strings.Split(logs, "\n")
			if len(lines) > 60 {
				lines = lines[len(lines)-60:]
			}
			log.Printf("--- logs %s/%s (tail):\n%s", p.Namespace, p.Name, strings.Join(lines, "\n"))
		}
	}
	events := &corev1.EventList{}
	if err := s.c.List(ctx, events); err == nil {
		for _, e := range events.Items {
			if e.Type == corev1.EventTypeWarning {
				log.Printf("event %s/%s %s: %s", e.InvolvedObject.Namespace, e.InvolvedObject.Name, e.Reason, e.Message)
			}
		}
	}
	if cm := (&corev1.ConfigMap{}); s.c.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-custom"}, cm) == nil {
		log.Printf("coredns-custom keys: %v", keys(cm.Data))
	}
	hosts, _ := os.ReadFile(nodeHostsFile)
	log.Printf("node /etc/hosts:\n%s", hosts)
}

func condReason(c *metav1.Condition) string {
	if c == nil {
		return "<none>"
	}
	return c.Reason
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
