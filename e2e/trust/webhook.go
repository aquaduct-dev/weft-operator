// webhook.go — the "webhook" scenario (//e2e/trust:webhook_test): the
// trust-injection webhook against a real API server. It checks the
// MutatingWebhookConfiguration the operator registers, that pods are left
// alone until a reflector exists and then get the reflector CA mounted over
// every system bundle path, the namespace/label/annotation/Windows
// exemptions, on-demand bundle publishing in new namespaces, dry runs,
// failurePolicy Ignore while the operator is down (and the trust gate that
// catches the pods admitted then), a serving certificate that survives an
// operator restart, and an unrelated ConfigMap that happens to be named like
// the trust bundle.
package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
	"aquaduct.dev/weft-operator/internal/pki"
)

const (
	workNS      = "e2e-trust"
	foreignNS   = "e2e-foreign"
	reflName    = "trust"
	reflHost    = "trust.weft-e2e.test"
	operatorDep = "weft-operator"

	operatorManifest = "/etc/e2e/weft-operator.yaml"
	gatewayAPICRDs   = "/etc/e2e/gateway-api-crds.yaml"

	busyboxImage = "docker.io/rancher/mirrored-library-busybox:1.36.1"
	alpineImage  = "ghcr.io/aquaduct-dev/weft-operator-e2e-trust:e2e"

	mwcName       = "weft-operator-trust-injector"
	webhookSecret = "weft-operator-webhook-tls"
	optOutKey     = "weft.aquaduct.dev/inject-trust"
	injectedAnn   = "weft.aquaduct.dev/trusted-cas"
	volumeName    = "weft-trust-bundle"
	bundleCM      = "weft-trust-bundle"
	bundleKey     = "ca-certificates.crt"
	onDemandLog   = "Published trust bundle on demand"
	// bundleHeader precedes the reflector's CA in the injected bundle.
	bundleHeader = "# weft-operator WeftLocalDNSReflector " + reflName
)

var bundlePaths = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/cert.pem",
}

type webhookScenario struct {
	k *e2ekit.Kit
	// caFP is the reflector CA fingerprint injected pods must carry.
	caFP string
}

func webhookSteps(k *e2ekit.Kit) []e2ekit.Step {
	w := &webhookScenario{k: k}
	k.Diagnostics = w.diagnostics
	return []e2ekit.Step{
		{ID: "webhook.install", Label: "operator installed from the chart and trust webhook registered", Fn: w.install},
		{ID: "webhook.configuration", Label: "webhook registered for pod CREATE only, failurePolicy Ignore, NoneOnDryRun, 5s, IfNeeded, namespace/object selectors, caBundle verifying the serving cert", Fn: w.checkConfiguration},
		{ID: "webhook.no-reflector", Label: "without any reflector a new pod is admitted unmodified", Fn: w.checkNoReflector},
		{ID: "webhook.reflector-ready", Label: "a reflector with a routed hostname becomes Ready and publishes its CoreDNS override", Fn: w.reflectorReady},
		{ID: "webhook.injected", Label: "a new pod gets the annotation, the 0444 bundle volume and all three mounts in init and app containers, and reads the reflector CA at each path", Fn: w.checkInjected},
		{ID: "webhook.covered-mount", Label: "a container whose own mount covers a bundle path keeps it; the other paths are still injected", Fn: w.checkCoveredMount},
		{ID: "webhook.excluded-namespaces", Label: "pods in kube-system and weft-system are not injected", Fn: w.checkExcludedNamespaces},
		{ID: "webhook.opt-out", Label: "label opt-out is skipped by the API server, annotation opt-out by the handler, a windows pod is untouched; none holds the trust gate", Fn: w.checkOptOut},
		{ID: "webhook.new-namespace", Label: "a pod created right after its namespace starts with the bundle ConfigMap there", Fn: w.checkNewNamespace},
		{ID: "webhook.namespace-watch", Label: "a new namespace with no pods gets the bundle ConfigMap from the reflector", Fn: w.checkNamespaceWatch},
		{ID: "webhook.dry-run", Label: "a server-side dry run is injected and persists nothing; the webhook logs no on-demand publish for it", Fn: w.checkDryRun},
		{ID: "webhook.failure-ignore", Label: "with the operator down a pod is admitted uninjected; the trust gate names it and withdraws DNS until it is gone", Fn: w.checkFailureIgnore},
		{ID: "webhook.restart-stable", Label: "serving certificate Secret and caBundle unchanged across an operator restart; injection still works", Fn: w.checkRestartStable},
		{ID: "webhook.foreign-configmap", Label: "an unrelated ConfigMap named weft-trust-bundle is left alone and pods there are not pointed at it", Fn: w.checkForeignConfigMap},
	}
}

func (w *webhookScenario) install(ctx context.Context) error {
	return w.k.InstallOperator(ctx, e2ekit.OperatorInstall{CRDs: gatewayAPICRDs, Manifest: operatorManifest})
}

func (w *webhookScenario) checkConfiguration(ctx context.Context) error {
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Name: mwcName}, mwc); err != nil {
		return err
	}
	if len(mwc.Webhooks) != 1 {
		return fmt.Errorf("want 1 webhook, got %d", len(mwc.Webhooks))
	}
	wh := mwc.Webhooks[0]
	var errs []string
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Sprintf(format, args...))
		}
	}
	check(len(wh.Rules) == 1, "rules: %+v", wh.Rules)
	if len(wh.Rules) == 1 {
		r := wh.Rules[0]
		check(slices.Equal(r.Operations, []admissionregistrationv1.OperationType{admissionregistrationv1.Create}), "operations %v, want [CREATE]", r.Operations)
		check(slices.Equal(r.APIGroups, []string{""}) && slices.Equal(r.APIVersions, []string{"v1"}) && slices.Equal(r.Resources, []string{"pods"}), "rule targets %v/%v/%v, want core/v1/pods", r.APIGroups, r.APIVersions, r.Resources)
	}
	check(wh.FailurePolicy != nil && *wh.FailurePolicy == admissionregistrationv1.Ignore, "failurePolicy %v", deref(wh.FailurePolicy))
	check(wh.SideEffects != nil && *wh.SideEffects == admissionregistrationv1.SideEffectClassNoneOnDryRun, "sideEffects %v", deref(wh.SideEffects))
	check(wh.TimeoutSeconds != nil && *wh.TimeoutSeconds == 5, "timeoutSeconds %v", deref(wh.TimeoutSeconds))
	check(wh.ReinvocationPolicy != nil && *wh.ReinvocationPolicy == admissionregistrationv1.IfNeededReinvocationPolicy, "reinvocationPolicy %v", deref(wh.ReinvocationPolicy))
	check(selectorIs(wh.NamespaceSelector, "kubernetes.io/metadata.name", metav1.LabelSelectorOpNotIn, "kube-system", e2ekit.OperatorNS), "namespaceSelector %+v", wh.NamespaceSelector)
	check(selectorIs(wh.ObjectSelector, optOutKey, metav1.LabelSelectorOpNotIn, "false"), "objectSelector %+v", wh.ObjectSelector)
	svc := wh.ClientConfig.Service
	check(svc != nil && svc.Namespace == e2ekit.OperatorNS && svc.Name == "weft-operator-webhook" && svc.Path != nil && *svc.Path == "/inject-trust", "service %+v", svc)
	if len(errs) > 0 {
		return fmt.Errorf("webhook configuration: %s", strings.Join(errs, "; "))
	}

	// The caBundle must be the CA in the persisted Secret, and the serving
	// certificate there must chain to it for the Service DNS name.
	sec := &corev1.Secret{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: webhookSecret}, sec); err != nil {
		return err
	}
	if !bytes.Equal(wh.ClientConfig.CABundle, sec.Data["ca.crt"]) {
		return fmt.Errorf("caBundle is not the CA in %s", webhookSecret)
	}
	if err := verifyServing(wh.ClientConfig.CABundle, sec.Data[corev1.TLSCertKey], "weft-operator-webhook.weft-system.svc"); err != nil {
		return err
	}
	log.Printf("webhook configuration as documented; serving cert verifies against caBundle")
	return nil
}

func (w *webhookScenario) checkNoReflector(ctx context.Context) error {
	pod := sleeper(workNS, "before-reflector")
	if err := w.k.C.Create(ctx, pod); err != nil {
		return err
	}
	if err := uninjected(pod); err != nil {
		return err
	}
	// It must not linger: it would hold the trust gate closed later.
	return w.deletePod(ctx, pod)
}

func (w *webhookScenario) reflectorReady(ctx context.Context) error {
	// No backend needed: the proxy is "available" once it listens, and
	// that is all DNS publishing waits for.
	if err := w.k.C.Create(ctx, &weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: workNS, Name: "trust"},
		Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: "http://backend." + workNS + ".svc:80", DstURL: "https://" + reflHost + "/"},
	}); err != nil {
		return err
	}
	if err := w.k.C.Create(ctx, &weftv1alpha1.WeftLocalDNSReflector{
		ObjectMeta: metav1.ObjectMeta{Name: reflName},
		Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{reflHost}},
	}); err != nil {
		return err
	}
	if err := w.waitReady(ctx, 5*time.Minute); err != nil {
		return err
	}
	ca := &corev1.Secret{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: reflName + "-ca"}, ca); err != nil {
		return err
	}
	w.caFP = pki.Fingerprint(ca.Data[corev1.TLSCertKey])
	return nil
}

func (w *webhookScenario) checkInjected(ctx context.Context) error {
	// Every path must carry the reflector CA after the system roots, and
	// be read-only for everyone (0444).
	script := `set -e
for p in ` + strings.Join(bundlePaths, " ") + `; do
  grep -q '` + bundleHeader + `' "$p" || { echo "no reflector CA in $p"; exit 1; }
  n=$(grep -c 'BEGIN CERTIFICATE' "$p")
  [ "$n" -gt 20 ] || { echo "$p has only $n certificates"; exit 1; }
  m=$(stat -L -c %a "$p")
  [ "$m" = 444 ] || { echo "$p mode $m"; exit 1; }
  echo "$p: $n certificates, mode $m, reflector CA present"
done`
	pod := sleeper(workNS, "injected")
	pod.Spec.InitContainers = []corev1.Container{{Name: "init", Image: busyboxImage, ImagePullPolicy: corev1.PullNever, Command: []string{"sh", "-c", script}}}
	pod.Spec.Containers[0].Command = []string{"sh", "-c", script}
	logs, err := w.k.RunPod(ctx, pod, 3*time.Minute)
	if err != nil {
		return err
	}
	got := &corev1.Pod{}
	if err := w.k.C.Get(ctx, client.ObjectKeyFromObject(pod), got); err != nil {
		return err
	}
	if got.Annotations[injectedAnn] != w.caFP {
		return fmt.Errorf("annotation %s=%q, want %q", injectedAnn, got.Annotations[injectedAnn], w.caFP)
	}
	if err := bundleVolume(got); err != nil {
		return err
	}
	for _, c := range append(append([]corev1.Container{}, got.Spec.InitContainers...), got.Spec.Containers...) {
		for _, p := range bundlePaths {
			if !hasBundleMount(c, p) {
				return fmt.Errorf("container %s has no read-only bundle mount at %s: %+v", c.Name, p, c.VolumeMounts)
			}
		}
	}
	if !strings.Contains(logs, "reflector CA present") {
		return fmt.Errorf("check script did not run")
	}
	return nil
}

func (w *webhookScenario) checkCoveredMount(ctx context.Context) error {
	pod := sleeper(workNS, "covered")
	pod.Spec.Volumes = []corev1.Volume{{Name: "own-pki", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "own-pki", MountPath: "/etc/pki"}}
	// The container's own (empty) /etc/pki wins; the ssl paths are ours.
	pod.Spec.Containers[0].Command = []string{"sh", "-c", `set -e
grep -q '` + bundleHeader + `' /etc/ssl/certs/ca-certificates.crt
grep -q '` + bundleHeader + `' /etc/ssl/cert.pem
[ ! -e /etc/pki/tls/certs/ca-bundle.crt ] || { echo "/etc/pki was overlaid"; exit 1; }
echo "own /etc/pki kept"`}
	if _, err := w.k.RunPod(ctx, pod, 3*time.Minute); err != nil {
		return err
	}
	got := &corev1.Pod{}
	if err := w.k.C.Get(ctx, client.ObjectKeyFromObject(pod), got); err != nil {
		return err
	}
	c := got.Spec.Containers[0]
	own := slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == "own-pki" && m.MountPath == "/etc/pki" })
	switch {
	case !own:
		return fmt.Errorf("own /etc/pki mount lost: %+v", c.VolumeMounts)
	case slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.MountPath == "/etc/pki/tls/certs/ca-bundle.crt" }):
		return fmt.Errorf("bundle mounted under the container's own /etc/pki: %+v", c.VolumeMounts)
	case !hasBundleMount(c, "/etc/ssl/certs/ca-certificates.crt") || !hasBundleMount(c, "/etc/ssl/cert.pem"):
		return fmt.Errorf("uncovered paths not injected: %+v", c.VolumeMounts)
	case got.Annotations[injectedAnn] != w.caFP:
		return fmt.Errorf("annotation %q", got.Annotations[injectedAnn])
	}
	return nil
}

func (w *webhookScenario) checkExcludedNamespaces(ctx context.Context) error {
	for _, ns := range []string{"kube-system", e2ekit.OperatorNS} {
		pod := sleeper(ns, "excluded")
		if err := w.k.C.Create(ctx, pod); err != nil {
			return err
		}
		if err := uninjected(pod); err != nil {
			return fmt.Errorf("namespace %s: %w", ns, err)
		}
		if err := w.deletePod(ctx, pod); err != nil {
			return err
		}
	}
	return nil
}

func (w *webhookScenario) checkOptOut(ctx context.Context) error {
	label := sleeper(workNS, "optout-label")
	label.Labels = map[string]string{optOutKey: "false"}
	annot := sleeper(workNS, "optout-annotation")
	annot.Annotations = map[string]string{optOutKey: "false"}
	windows := sleeper(workNS, "windows")
	windows.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}

	// The counter tells who decided: the objectSelector keeps a labelled
	// pod from ever reaching the handler; an annotation is only visible to
	// the handler.
	type tc struct {
		pod         *corev1.Pod
		wantReached bool
	}
	for _, c := range []tc{{label, false}, {annot, true}, {windows, true}} {
		before, err := webhookRequests(ctx)
		if err != nil {
			return fmt.Errorf("read webhook metrics: %w", err)
		}
		if err := w.k.C.Create(ctx, c.pod); err != nil {
			return err
		}
		after, err := webhookRequests(ctx)
		if err != nil {
			return err
		}
		log.Printf("pod %s: webhook requests %d -> %d", c.pod.Name, before, after)
		if reached := after > before; reached != c.wantReached {
			return fmt.Errorf("pod %s reached the webhook=%v (requests %d -> %d), want %v", c.pod.Name, reached, before, after, c.wantReached)
		}
		if err := uninjected(c.pod); err != nil {
			return fmt.Errorf("pod %s: %w", c.pod.Name, err)
		}
	}

	// The opted-out pods run without the CA, and must not hold the gate;
	// the windows one cannot schedule here and stays Pending.
	for _, p := range []*corev1.Pod{label, annot} {
		if err := e2ekit.WaitFor(ctx, 2*time.Minute, p.Name+" running", w.podPhase(p, corev1.PodRunning)); err != nil {
			return err
		}
	}
	if err := e2ekit.Holds(ctx, 40*time.Second, "reflector stays Ready with opted-out pods running", func() (bool, string, error) {
		r, err := w.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		ready := e2ekit.Condition(r.Status.Conditions, "Ready")
		return ready != nil && ready.Status == metav1.ConditionTrue && r.Status.UntrustedPods == 0 && w.coreDNSHasKey(ctx),
			fmt.Sprintf("%s untrusted=%d %v", e2ekit.CondString(ready), r.Status.UntrustedPods, r.Status.UntrustedPodSample), nil
	}); err != nil {
		return err
	}
	if err := e2ekit.Holds(ctx, 10*time.Second, "windows pod stays Pending", w.podPhase(windows, corev1.PodPending)); err != nil {
		return err
	}
	for _, p := range []*corev1.Pod{label, annot, windows} {
		if err := w.deletePod(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (w *webhookScenario) checkNewNamespace(ctx context.Context) error {
	ns := "zz-fresh"
	if err := w.k.C.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		return err
	}
	pod := sleeper(ns, "first")
	if err := w.k.C.Create(ctx, pod); err != nil {
		return err
	}
	if pod.Annotations[injectedAnn] != w.caFP {
		return fmt.Errorf("pod in new namespace not injected: %v", pod.Annotations)
	}
	// Running means the kubelet found the ConfigMap the pod mounts.
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "pod in new namespace running", w.podPhase(pod, corev1.PodRunning)); err != nil {
		return err
	}
	if err := w.ownBundle(ctx, ns); err != nil {
		return err
	}
	logs, _ := w.k.OperatorLogs(ctx)
	log.Printf("bundle in %s published by the webhook on demand: %v (else by the reflector's namespace watch)", ns, onDemandFor(logs, ns))
	return w.deletePod(ctx, pod)
}

func (w *webhookScenario) checkNamespaceWatch(ctx context.Context) error {
	ns := "zz-empty"
	if err := w.k.C.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "bundle ConfigMap in "+ns, func() (bool, string, error) {
		err := w.ownBundle(ctx, ns)
		return err == nil, fmt.Sprint(err), nil
	})
}

func (w *webhookScenario) checkDryRun(ctx context.Context) error {
	ns := "zz-dryrun"
	if err := w.k.C.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		return err
	}
	pod := sleeper(ns, "dry")
	if err := w.k.C.Create(ctx, pod, client.DryRunAll); err != nil {
		return err
	}
	if pod.Annotations[injectedAnn] != w.caFP {
		return fmt.Errorf("dry-run response not injected: %v", pod.Annotations)
	}
	if err := w.k.C.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); !apierrors.IsNotFound(err) {
		return fmt.Errorf("dry-run pod exists (err=%v)", err)
	}
	// The reflector's namespace watch usually publishes the bundle in a new
	// namespace within milliseconds, so the ConfigMap's existence proves
	// nothing either way; what the webhook did is in its log.
	logs, err := w.k.OperatorLogs(ctx)
	if err != nil {
		return err
	}
	if onDemandFor(logs, ns) {
		return fmt.Errorf("webhook published the bundle in %s on a dry run", ns)
	}
	return nil
}

func (w *webhookScenario) checkFailureIgnore(ctx context.Context) error {
	if err := w.scaleOperator(ctx, 0); err != nil {
		return err
	}
	pod := sleeper(workNS, "while-down")
	start := time.Now()
	if err := w.k.C.Create(ctx, pod); err != nil {
		return fmt.Errorf("pod not admitted with the webhook down: %w", err)
	}
	log.Printf("pod admitted with the operator down in %s", time.Since(start).Round(time.Millisecond))
	if err := uninjected(pod); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "pod admitted while down running", w.podPhase(pod, corev1.PodRunning)); err != nil {
		return err
	}
	if err := w.scaleOperator(ctx, 1); err != nil {
		return err
	}
	want := workNS + "/" + pod.Name
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "trust gate closed on "+want+" and DNS withdrawn", func() (bool, string, error) {
		r, err := w.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		tr := e2ekit.Condition(r.Status.Conditions, "ClientsTrustCA")
		dns := e2ekit.Condition(r.Status.Conditions, "DNSPublished")
		key := w.coreDNSHasKey(ctx)
		ok := tr != nil && tr.Status == metav1.ConditionFalse && tr.Reason == "PodsNeedRestart" && strings.Contains(tr.Message, want) &&
			slices.Contains(r.Status.UntrustedPodSample, want) &&
			dns != nil && dns.Status == metav1.ConditionFalse && dns.Reason == "WaitingForTrust" && !key
		return ok, fmt.Sprintf("%s %s coredns-key=%v sample=%v", e2ekit.CondString(tr), e2ekit.CondString(dns), key, r.Status.UntrustedPodSample), nil
	}); err != nil {
		return err
	}
	if err := w.deletePod(ctx, pod); err != nil {
		return err
	}
	return w.waitReady(ctx, 3*time.Minute)
}

func (w *webhookScenario) checkRestartStable(ctx context.Context) error {
	secBefore := &corev1.Secret{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: webhookSecret}, secBefore); err != nil {
		return err
	}
	bundleBefore, err := w.caBundle(ctx)
	if err != nil {
		return err
	}
	old, err := w.operatorPod(ctx)
	if err != nil {
		return err
	}
	if err := w.k.C.Delete(ctx, old); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "old operator pod gone", w.k.Gone(&corev1.Pod{}, old.Namespace, old.Name)); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "replacement operator pod ready", func() (bool, string, error) {
		p, err := w.operatorPod(ctx)
		if err != nil {
			return false, err.Error(), nil
		}
		return p.UID != old.UID && podReady(p), p.Name + " " + e2ekit.ContainerState(p), nil
	}); err != nil {
		return err
	}
	secAfter := &corev1.Secret{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: webhookSecret}, secAfter); err != nil {
		return err
	}
	for _, key := range []string{"ca.crt", "ca.key", corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
		if !bytes.Equal(secBefore.Data[key], secAfter.Data[key]) {
			return fmt.Errorf("%s/%s changed across the restart", webhookSecret, key)
		}
	}
	bundleAfter, err := w.caBundle(ctx)
	if err != nil {
		return err
	}
	if !bytes.Equal(bundleBefore, bundleAfter) {
		return fmt.Errorf("MWC caBundle changed across the restart")
	}
	// The API server must trust the new pod's serving cert at once.
	return e2ekit.WaitFor(ctx, time.Minute, "pod injected by the restarted operator", func() (bool, string, error) {
		pod := sleeper(workNS, "after-restart")
		_ = w.deletePod(ctx, pod)
		if err := w.k.C.Create(ctx, pod); err != nil {
			return false, "", err
		}
		ok := pod.Annotations[injectedAnn] == w.caFP
		if err := w.deletePod(ctx, pod); err != nil {
			return false, "", err
		}
		return ok, fmt.Sprintf("annotations=%v", pod.Annotations), nil
	})
}

func (w *webhookScenario) checkForeignConfigMap(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: foreignNS, Name: bundleCM}, cm); err != nil {
		return err
	}
	if len(cm.Data) != 1 || cm.Data["owner"] != "someone-else" || len(cm.OwnerReferences) != 0 || cm.Labels["app.kubernetes.io/name"] != "" {
		return fmt.Errorf("foreign ConfigMap was modified: labels=%v owners=%v data keys=%v", cm.Labels, cm.OwnerReferences, keys(cm.Data))
	}
	// An alpine pod (a real system trust store at the bundle paths) must
	// start: the webhook may not mount someone else's ConfigMap over it.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: foreignNS, Name: "workload"},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: e2ekit.Ptr(false),
			Containers: []corev1.Container{{
				Name: "app", Image: alpineImage, ImagePullPolicy: corev1.PullNever, Args: []string{"sleep"},
			}},
		},
	}
	if err := w.k.C.Create(ctx, pod); err != nil {
		return err
	}
	if slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool { return v.Name == volumeName }) {
		return fmt.Errorf("pod in %s mounts the unrelated ConfigMap: %+v", foreignNS, pod.Spec.Volumes)
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "pod next to the foreign ConfigMap running", w.podPhase(pod, corev1.PodRunning)); err != nil {
		return err
	}
	// It does not trust the CA, so the gate must hold DNS back for it.
	want := foreignNS + "/" + pod.Name
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "trust gate names "+want, func() (bool, string, error) {
		r, err := w.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		tr := e2ekit.Condition(r.Status.Conditions, "ClientsTrustCA")
		return tr != nil && tr.Reason == "PodsNeedRestart" && slices.Contains(r.Status.UntrustedPodSample, want) && !w.coreDNSHasKey(ctx),
			fmt.Sprintf("%s sample=%v", e2ekit.CondString(tr), r.Status.UntrustedPodSample), nil
	}); err != nil {
		return err
	}
	if err := w.k.C.Get(ctx, client.ObjectKeyFromObject(cm), cm); err != nil {
		return err
	}
	if len(cm.Data) != 1 || cm.Data["owner"] != "someone-else" || len(cm.OwnerReferences) != 0 {
		return fmt.Errorf("foreign ConfigMap was modified later: owners=%v data keys=%v", cm.OwnerReferences, keys(cm.Data))
	}
	return nil
}

// --- helpers -----------------------------------------------------------------

func sleeper(ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  e2ekit.Ptr(false),
			TerminationGracePeriodSeconds: e2ekit.Ptr(int64(0)),
			Containers: []corev1.Container{{
				Name: "app", Image: busyboxImage, ImagePullPolicy: corev1.PullNever, Command: []string{"sleep", "3600"},
			}},
		},
	}
}

// uninjected checks an admitted pod carries nothing from the webhook.
func uninjected(p *corev1.Pod) error {
	if _, ok := p.Annotations[injectedAnn]; ok {
		return fmt.Errorf("pod %s/%s has %s", p.Namespace, p.Name, injectedAnn)
	}
	if slices.ContainsFunc(p.Spec.Volumes, func(v corev1.Volume) bool { return v.Name == volumeName }) {
		return fmt.Errorf("pod %s/%s has the %s volume", p.Namespace, p.Name, volumeName)
	}
	for _, c := range append(append([]corev1.Container{}, p.Spec.InitContainers...), p.Spec.Containers...) {
		if slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == volumeName }) {
			return fmt.Errorf("pod %s/%s container %s mounts %s", p.Namespace, p.Name, c.Name, volumeName)
		}
	}
	return nil
}

func bundleVolume(p *corev1.Pod) error {
	for _, v := range p.Spec.Volumes {
		if v.Name != volumeName {
			continue
		}
		if v.ConfigMap == nil || v.ConfigMap.Name != bundleCM || v.ConfigMap.DefaultMode == nil || *v.ConfigMap.DefaultMode != 0o444 {
			return fmt.Errorf("bundle volume %+v, want ConfigMap %s mode 0444", v.VolumeSource, bundleCM)
		}
		return nil
	}
	return fmt.Errorf("no %s volume", volumeName)
}

func hasBundleMount(c corev1.Container, path string) bool {
	return slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool {
		return m.Name == volumeName && m.MountPath == path && m.SubPath == bundleKey && m.ReadOnly
	})
}

func selectorIs(s *metav1.LabelSelector, key string, op metav1.LabelSelectorOperator, values ...string) bool {
	if s == nil || len(s.MatchLabels) != 0 || len(s.MatchExpressions) != 1 {
		return false
	}
	e := s.MatchExpressions[0]
	got := slices.Clone(e.Values)
	slices.Sort(got)
	want := slices.Clone(values)
	slices.Sort(want)
	return e.Key == key && e.Operator == op && slices.Equal(got, want)
}

func verifyServing(caPEM, certPEM []byte, dnsName string) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("caBundle holds no certificate")
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("serving certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: dnsName}); err != nil {
		return fmt.Errorf("serving certificate does not verify for %s: %w", dnsName, err)
	}
	return nil
}

// ownBundle checks ns holds the operator's trust bundle, owned by the reflector.
func (w *webhookScenario) ownBundle(ctx context.Context, ns string) error {
	cm := &corev1.ConfigMap{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: bundleCM}, cm); err != nil {
		return err
	}
	owned := slices.ContainsFunc(cm.OwnerReferences, func(o metav1.OwnerReference) bool {
		return o.Kind == "WeftLocalDNSReflector" && o.Name == reflName
	})
	if cm.Labels["app.kubernetes.io/name"] != "weft-local-dns-reflector" || !owned || !strings.Contains(cm.Data[bundleKey], bundleHeader) {
		return fmt.Errorf("%s/%s is not the operator's bundle: labels=%v owners=%v", ns, bundleCM, cm.Labels, cm.OwnerReferences)
	}
	return nil
}

func onDemandFor(logs, ns string) bool {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, onDemandLog) && strings.Contains(line, `"`+ns+`"`) {
			return true
		}
	}
	return false
}

func (w *webhookScenario) deletePod(ctx context.Context, p *corev1.Pod) error {
	err := w.k.C.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name}}, client.GracePeriodSeconds(0))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "pod "+p.Name+" deleted", w.k.Gone(&corev1.Pod{}, p.Namespace, p.Name))
}

func (w *webhookScenario) podPhase(p *corev1.Pod, phase corev1.PodPhase) func() (bool, string, error) {
	return func() (bool, string, error) {
		got := &corev1.Pod{}
		if err := w.k.C.Get(context.Background(), client.ObjectKeyFromObject(p), got); err != nil {
			return false, "", err
		}
		return got.Status.Phase == phase, string(got.Status.Phase) + " " + e2ekit.ContainerState(got), nil
	}
}

func (w *webhookScenario) reflector(ctx context.Context) (*weftv1alpha1.WeftLocalDNSReflector, error) {
	r := &weftv1alpha1.WeftLocalDNSReflector{}
	return r, w.k.C.Get(ctx, types.NamespacedName{Name: reflName}, r)
}

func (w *webhookScenario) waitReady(ctx context.Context, timeout time.Duration) error {
	return e2ekit.WaitFor(ctx, timeout, "reflector Ready with its CoreDNS override", func() (bool, string, error) {
		r, err := w.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		ready := e2ekit.Condition(r.Status.Conditions, "Ready")
		key := w.coreDNSHasKey(ctx)
		return ready != nil && ready.Status == metav1.ConditionTrue && key, condSummary(r) + fmt.Sprintf(" coredns-key=%v", key), nil
	})
}

func (w *webhookScenario) coreDNSHasKey(ctx context.Context) bool {
	cm := &corev1.ConfigMap{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-custom"}, cm); err != nil {
		return false
	}
	_, ok := cm.Data["weft-local-dns-reflector-"+reflName+".server"]
	return ok
}

func (w *webhookScenario) caBundle(ctx context.Context) ([]byte, error) {
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Name: mwcName}, mwc); err != nil {
		return nil, err
	}
	if len(mwc.Webhooks) != 1 {
		return nil, fmt.Errorf("%d webhooks", len(mwc.Webhooks))
	}
	return mwc.Webhooks[0].ClientConfig.CABundle, nil
}

func (w *webhookScenario) operatorPod(ctx context.Context) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := w.k.C.List(ctx, pods, client.InNamespace(e2ekit.OperatorNS), client.MatchingLabels{"app.kubernetes.io/name": "weft-operator"}); err != nil {
		return nil, err
	}
	var live []corev1.Pod
	for _, p := range pods.Items {
		if p.DeletionTimestamp == nil {
			live = append(live, p)
		}
	}
	if len(live) != 1 {
		return nil, fmt.Errorf("%d live operator pods", len(live))
	}
	return &live[0], nil
}

func (w *webhookScenario) scaleOperator(ctx context.Context, replicas int32) error {
	dep := &appsv1.Deployment{}
	if err := w.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: operatorDep}, dep); err != nil {
		return err
	}
	patch := client.MergeFrom(dep.DeepCopy())
	dep.Spec.Replicas = &replicas
	if err := w.k.C.Patch(ctx, dep, patch); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 3*time.Minute, fmt.Sprintf("operator at %d replicas", replicas), func() (bool, string, error) {
		pods := &corev1.PodList{}
		if err := w.k.C.List(ctx, pods, client.InNamespace(e2ekit.OperatorNS), client.MatchingLabels{"app.kubernetes.io/name": "weft-operator"}); err != nil {
			return false, "", err
		}
		ready := 0
		for _, p := range pods.Items {
			if p.DeletionTimestamp == nil && podReady(&p) {
				ready++
			}
		}
		if replicas == 0 {
			return len(pods.Items) == 0, fmt.Sprintf("%d pods", len(pods.Items)), nil
		}
		return ready == int(replicas), fmt.Sprintf("%d/%d ready", ready, len(pods.Items)), nil
	})
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func condSummary(r *weftv1alpha1.WeftLocalDNSReflector) string {
	var parts []string
	for _, c := range r.Status.Conditions {
		parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
	}
	return strings.Join(parts, " ")
}

func (w *webhookScenario) diagnostics(ctx context.Context) {
	if r, err := w.reflector(ctx); err == nil {
		log.Printf("reflector %s: %s untrusted=%d %v", reflName, condSummary(r), r.Status.UntrustedPods, r.Status.UntrustedPodSample)
		for _, c := range r.Status.Conditions {
			log.Printf("  %s=%s %s: %s", c.Type, c.Status, c.Reason, c.Message)
		}
	}
	cms := &corev1.ConfigMapList{}
	if err := w.k.C.List(ctx, cms); err == nil {
		for _, cm := range cms.Items {
			if cm.Name == bundleCM {
				log.Printf("configmap %s/%s labels=%v owners=%d", cm.Namespace, cm.Name, cm.Labels, len(cm.OwnerReferences))
			}
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func deref[T any](p *T) any {
	if p == nil {
		return "<nil>"
	}
	return *p
}
