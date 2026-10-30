// extras.go — shared helpers for the e2ekit-based reflector scenarios:
// "reflector-claims" (claims_scenario.go) and "reflector-lifecycle"
// (lifecycle_scenario.go). They reuse the original scenario's registry
// backend and client image, and add status/node-file assertions.
package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

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

var extraScenarios = e2ekit.Scenarios{
	"reflector-claims":    claimsSteps,
	"reflector-lifecycle": lifecycleSteps,
}

const (
	busyboxImage = "docker.io/rancher/mirrored-library-busybox:1.36.1"
	optOutKey    = "weft.aquaduct.dev/inject-trust"
	injectedAnn  = "weft.aquaduct.dev/trusted-cas"
	registrySrc  = "http://registry." + backendNS + ".svc:5000"
)

// extras is the state shared by the e2ekit reflector scenarios.
type extras struct {
	k *e2ekit.Kit
	// legacy is the original scenario helper, for its registry backend.
	legacy *scenario
}

func newExtras(k *e2ekit.Kit) *extras {
	return &extras{k: k, legacy: &scenario{kubeconfig: k.Kubeconfig, c: k.C, clientset: k.CS}}
}

func (x *extras) install(ctx context.Context) error {
	pristine, err := os.ReadFile(nodeCABundle)
	if err != nil {
		return fmt.Errorf("node has no system trust store: %w", err)
	}
	if err := os.WriteFile(pristineCABundle, pristine, 0o644); err != nil {
		return err
	}
	return x.k.InstallOperator(ctx, e2ekit.OperatorInstall{CRDs: gatewayAPICRDs, Manifest: operatorManifest})
}

func (x *extras) reflector(ctx context.Context, name string) (*weftv1alpha1.WeftLocalDNSReflector, error) {
	r := &weftv1alpha1.WeftLocalDNSReflector{}
	return r, x.k.C.Get(ctx, types.NamespacedName{Name: name}, r)
}

func (x *extras) createReflector(ctx context.Context, name string, spec weftv1alpha1.WeftLocalDNSReflectorSpec) error {
	return x.k.C.Create(ctx, &weftv1alpha1.WeftLocalDNSReflector{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: spec})
}

// patchReflector applies mutate to the reflector's spec with a merge patch.
func (x *extras) patchReflector(ctx context.Context, name string, mutate func(*weftv1alpha1.WeftLocalDNSReflectorSpec)) error {
	r, err := x.reflector(ctx, name)
	if err != nil {
		return err
	}
	patch := client.MergeFrom(r.DeepCopy())
	mutate(&r.Spec)
	return x.k.C.Patch(ctx, r, patch)
}

func (x *extras) createTunnel(ctx context.Context, name, src, dst string) error {
	return x.k.C.Create(ctx, &weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: backendNS, Name: name},
		Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: src, DstURL: dst},
	})
}

// waitStatus polls a reflector until check (given a status observed for its
// current generation) passes.
func (x *extras) waitStatus(ctx context.Context, name string, timeout time.Duration, what string, check func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string)) error {
	return e2ekit.WaitFor(ctx, timeout, name+": "+what, func() (bool, string, error) {
		r, err := x.reflector(ctx, name)
		if err != nil {
			return false, "", err
		}
		if r.Status.ObservedGeneration != r.Generation {
			return false, fmt.Sprintf("observedGeneration %d < %d", r.Status.ObservedGeneration, r.Generation), nil
		}
		ok, state := check(r)
		return ok, state + " | " + condSummary(r), nil
	})
}

// condIs reports whether condition t has status s (and reason, if given).
func condIs(r *weftv1alpha1.WeftLocalDNSReflector, t string, s metav1.ConditionStatus, reason string) bool {
	c := e2ekit.Condition(r.Status.Conditions, t)
	return c != nil && c.Status == s && (reason == "" || c.Reason == reason)
}

func condSummary(r *weftv1alpha1.WeftLocalDNSReflector) string {
	var parts []string
	for _, c := range r.Status.Conditions {
		parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
	}
	return strings.Join(parts, " ")
}

// coreDNSKeys lists the reflector keys present in coredns-custom.
func (x *extras) coreDNSKeys(ctx context.Context) []string {
	cm := &corev1.ConfigMap{}
	if err := x.k.C.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-custom"}, cm); err != nil {
		return nil
	}
	var out []string
	for k := range cm.Data {
		if name, ok := strings.CutPrefix(k, "weft-local-dns-reflector-"); ok {
			out = append(out, strings.TrimSuffix(name, ".server"))
		}
	}
	slices.Sort(out)
	return out
}

func (x *extras) hasCoreDNSKey(ctx context.Context, refl string) bool {
	return slices.Contains(x.coreDNSKeys(ctx), refl)
}

// caFingerprint is the fingerprint of a reflector's CA certificate.
func (x *extras) caFingerprint(ctx context.Context, refl string) (string, error) {
	s := &corev1.Secret{}
	if err := x.k.C.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: refl + "-ca"}, s); err != nil {
		return "", err
	}
	return pki.Fingerprint(s.Data[corev1.TLSCertKey]), nil
}

// waitCA waits until a reflector's CA Secret exists and returns its fingerprint.
func (x *extras) waitCA(ctx context.Context, refl string) (string, error) {
	var fp string
	err := e2ekit.WaitFor(ctx, 2*time.Minute, refl+" CA issued", func() (bool, string, error) {
		var err error
		fp, err = x.caFingerprint(ctx, refl)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		return err == nil, "present", err
	})
	return fp, err
}

// restartRegistry replaces the registry pod and waits for one that runs
// with every fingerprint in fps injected.
func (x *extras) restartRegistry(ctx context.Context, fps ...string) error {
	if err := x.k.C.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(backendNS), client.MatchingLabels{"app": "registry"}, client.GracePeriodSeconds(0)); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 3*time.Minute, "registry pod restarted trusting "+strings.Join(fps, ","), func() (bool, string, error) {
		pods := &corev1.PodList{}
		if err := x.k.C.List(ctx, pods, client.InNamespace(backendNS), client.MatchingLabels{"app": "registry"}); err != nil {
			return false, "", err
		}
		var states []string
		ready := false
		for _, p := range pods.Items {
			trusted := true
			for _, fp := range fps {
				trusted = trusted && slices.Contains(strings.Split(p.Annotations[injectedAnn], ","), fp)
			}
			states = append(states, fmt.Sprintf("%s %s deleting=%v trusted=%v", p.Name, p.Status.Phase, p.DeletionTimestamp != nil, trusted))
			if p.DeletionTimestamp != nil || !trusted || !podReady(&p) {
				return false, strings.Join(states, "; "), nil
			}
			ready = true
		}
		return ready, strings.Join(states, "; "), nil
	})
}

// sleeper creates a long-running busybox pod (not injected unless created
// after a reflector exists).
func (x *extras) sleeper(ctx context.Context, ns, name string, labels, annotations map[string]string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  e2ekit.Ptr(false),
			TerminationGracePeriodSeconds: e2ekit.Ptr(int64(0)),
			Containers: []corev1.Container{{
				Name: "app", Image: busyboxImage, ImagePullPolicy: corev1.PullNever, Command: []string{"sleep", "3600"},
			}},
		},
	}
	if err := x.k.C.Create(ctx, pod); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "pod "+ns+"/"+name+" running", func() (bool, string, error) {
		p := &corev1.Pod{}
		if err := x.k.C.Get(ctx, client.ObjectKeyFromObject(pod), p); err != nil {
			return false, "", err
		}
		return p.Status.Phase == corev1.PodRunning, string(p.Status.Phase), nil
	})
}

func (x *extras) deletePod(ctx context.Context, ns, name string) error {
	err := x.k.C.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}, client.GracePeriodSeconds(0))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "pod "+ns+"/"+name+" deleted", x.k.Gone(&corev1.Pod{}, ns, name))
}

// clientPod runs the client image with args (retrying while CoreDNS reloads
// the override) until it succeeds.
func (x *extras) clientPod(ctx context.Context, name string, args ...string) error {
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: clientNS, Name: name},
			Spec: corev1.PodSpec{
				AutomountServiceAccountToken: e2ekit.Ptr(false),
				Containers: []corev1.Container{{
					Name: "client", Image: clientImage, ImagePullPolicy: corev1.PullNever, Args: args,
				}},
			},
		}
		if _, lastErr = x.k.RunPod(ctx, pod, 4*time.Minute); lastErr == nil {
			return x.deletePod(ctx, clientNS, name)
		}
		log.Printf("client %s attempt %d failed: %v; retrying", name, attempt, lastErr)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	return lastErr
}

// servingCert parses a reflector's current serving certificate.
func (x *extras) servingCert(ctx context.Context, refl string) (*x509.Certificate, string, error) {
	s := &corev1.Secret{}
	if err := x.k.C.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: refl + "-tls"}, s); err != nil {
		return nil, "", err
	}
	block, _ := pem.Decode(s.Data[corev1.TLSCertKey])
	if block == nil {
		return nil, "", fmt.Errorf("%s-tls holds no PEM certificate", refl)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return cert, pki.Fingerprint(s.Data[corev1.TLSCertKey]), err
}

func (x *extras) proxyDeployment(ctx context.Context, refl string) (*appsv1.Deployment, error) {
	d := &appsv1.Deployment{}
	return d, x.k.C.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: refl + "-proxy"}, d)
}

func (x *extras) proxyPods(ctx context.Context, refl string) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	err := x.k.C.List(ctx, pods, client.InNamespace(operatorNS), client.MatchingLabels{"weft.aquaduct.dev/reflector": refl, "app.kubernetes.io/component": "proxy"})
	return pods.Items, err
}

func (x *extras) nodeAgentExists(ctx context.Context, refl string) (bool, error) {
	err := x.k.C.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: refl + "-node-agent"}, &appsv1.DaemonSet{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Node files: the agent's markers (see internal/nodeagent/hostsfile.go).
func hostsBegin(refl string) string { return "# BEGIN weft-local-dns-reflector " + refl + " (" }
func hostsEnd(refl string) string   { return "# END weft-local-dns-reflector " + refl }
func caBegin(refl string) string    { return "# BEGIN weft-local-dns-reflector " + refl + " CA (" }
func caEnd(refl string) string      { return "# END weft-local-dns-reflector " + refl + " CA" }

// nodeHostsLine is the agent's /etc/hosts entry for refl ("" if absent).
func nodeHostsLine(refl string) string {
	b, _ := os.ReadFile(nodeHostsFile)
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, hostsBegin(refl)) && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

func nodeTrustHasCA(refl string) bool {
	b, _ := os.ReadFile(nodeCABundle)
	return strings.Contains(string(b), caBegin(refl))
}

// removeBlock deletes the begin..end block from a node file in place (the
// way an admin or a config-management run would clobber it).
func removeBlock(path, beginPrefix, end string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out []string
	in, found := false, false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case !in && strings.HasPrefix(l, beginPrefix):
			in, found = true, true
		case in && l == end:
			in = false
		case !in:
			out = append(out, l)
		}
	}
	if !found {
		return fmt.Errorf("%s has no %q block", path, beginPrefix)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// diagnostics dumps every reflector, the CoreDNS keys and node files.
func (x *extras) diagnostics(ctx context.Context) {
	list := &weftv1alpha1.WeftLocalDNSReflectorList{}
	if err := x.k.C.List(ctx, list); err == nil {
		for _, r := range list.Items {
			log.Printf("reflector %s: reflected=%v conflicting=%v unrouted=%v routes=%v untrusted=%d %v ip=%s",
				r.Name, r.Status.ReflectedHostnames, r.Status.ConflictingHostnames, r.Status.UnroutedHostnames, r.Status.Routes, r.Status.UntrustedPods, r.Status.UntrustedPodSample, r.Status.ServiceIP)
			for _, c := range r.Status.Conditions {
				log.Printf("  %s=%s %s: %s", c.Type, c.Status, c.Reason, c.Message)
			}
		}
	}
	log.Printf("coredns-custom reflector keys: %v", x.coreDNSKeys(ctx))
	hosts, _ := os.ReadFile(nodeHostsFile)
	log.Printf("node /etc/hosts:\n%s", hosts)
	for _, l := range []string{"app.kubernetes.io/component=proxy", "app.kubernetes.io/component=node-agent"} {
		pl, err := x.k.CS.CoreV1().Pods(operatorNS).List(ctx, metav1.ListOptions{LabelSelector: l})
		if err != nil {
			continue
		}
		for _, p := range pl.Items {
			logs, _ := x.k.PodLogs(ctx, p.Namespace, p.Name, "", false)
			log.Printf("--- logs %s/%s (tail):\n%s", p.Namespace, p.Name, e2ekit.Tail(logs, 40))
		}
	}
}
