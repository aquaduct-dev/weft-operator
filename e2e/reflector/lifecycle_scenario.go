// lifecycle_scenario.go — the "reflector-lifecycle" scenario
// (//e2e/reflector:reflector_lifecycle_test): one reflector through the
// changes a running cluster puts it through.
//
//   - a hostname added to the spec reissues the serving certificate (new
//     SANs and fingerprint) and rolls the proxy; once its tunnel exists the
//     new hostname verifies through the proxy;
//   - with the proxy unavailable (node cordoned, proxy pod deleted) the DNS
//     override is withdrawn (DNSPublished=False/Withdrawn) and the node
//     agent, whose TLS check now fails, removes its /etc/hosts entry; both
//     come back when the proxy does;
//   - the node agent re-asserts its /etc/hosts block, node trust-store block
//     and containerd certs.d entry after they are deleted on the node;
//   - clusterDNS.disabled removes the CoreDNS override (DNSPublished=True/
//     Disabled, pods stop resolving the name) and nodeTrust.disabled removes
//     the node agent and everything it wrote on the node.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	reflLife = "life"
	hostB    = "b.weft-e2e.test"
)

type lifecycleScenario struct {
	*extras
	serviceIP string
}

func lifecycleSteps(k *e2ekit.Kit) []e2ekit.Step {
	l := &lifecycleScenario{extras: newExtras(k)}
	k.Diagnostics = l.diagnostics
	return []e2ekit.Step{
		{ID: "life.install", Label: "operator installed from the chart (no default reflector)", Fn: l.install},
		{ID: "life.ready", Label: "a reflector for one tunnelled hostname is Ready, with the node agent's entries on the node", Fn: l.ready},
		{ID: "life.reissue", Label: "adding a hostname reissues the serving cert with the new SAN and rolls the proxy; the new hostname then verifies", Fn: l.reissue},
		{ID: "life.proxy-down", Label: "proxy unavailable: DNS withdrawn and the node agent drops its /etc/hosts entry; both restored when it is back", Fn: l.proxyDown},
		{ID: "life.agent-reasserts", Label: "node agent restores its /etc/hosts block, trust-store block and certs.d entry after they are deleted on the node", Fn: l.agentReasserts},
		{ID: "life.cluster-dns-disabled", Label: "clusterDNS.disabled removes the CoreDNS override (DNSPublished=True/Disabled) and pods stop resolving the name", Fn: l.clusterDNSDisabled},
		{ID: "life.node-trust-disabled", Label: "nodeTrust.disabled removes the node agent and everything it wrote on the node", Fn: l.nodeTrustDisabled},
	}
}

func (l *lifecycleScenario) ready(ctx context.Context) error {
	if err := l.legacy.deployBackend(ctx); err != nil {
		return err
	}
	if err := l.createTunnel(ctx, "a", registrySrc, "https://"+hostA+"/"); err != nil {
		return err
	}
	if err := l.createReflector(ctx, reflLife, weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{hostA}}); err != nil {
		return err
	}
	fp, err := l.waitCA(ctx, reflLife)
	if err != nil {
		return err
	}
	// The registry predates the reflector; replace it so the gate opens.
	if err := l.restartRegistry(ctx, fp); err != nil {
		return err
	}
	if err := l.waitReady(ctx, 5*time.Minute); err != nil {
		return err
	}
	if err := l.waitNode(ctx, 3*time.Minute, "node agent entries written", []string{hostA}); err != nil {
		return err
	}
	return l.clientPod(ctx, "ready", "client", "--host="+hostA, "--expect-ip="+l.serviceIP)
}

func (l *lifecycleScenario) reissue(ctx context.Context) error {
	certBefore, fpBefore, err := l.servingCert(ctx, reflLife)
	if err != nil {
		return err
	}
	podsBefore, err := l.proxyPods(ctx, reflLife)
	if err != nil {
		return err
	}
	if !slices.Equal(certBefore.DNSNames, []string{hostA}) {
		return fmt.Errorf("serving cert SANs %v before the change", certBefore.DNSNames)
	}
	if err := l.patchReflector(ctx, reflLife, func(s *weftv1alpha1.WeftLocalDNSReflectorSpec) { s.Hostnames = []string{hostA, hostB} }); err != nil {
		return err
	}
	// The certificate covers claimed hostnames, routed or not, so it is
	// reissued before b has a tunnel.
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "serving cert reissued for both hostnames and proxy rolled", func() (bool, string, error) {
		cert, fp, err := l.servingCert(ctx, reflLife)
		if err != nil {
			return false, "", err
		}
		dep, err := l.proxyDeployment(ctx, reflLife)
		if err != nil {
			return false, "", err
		}
		pods, err := l.proxyPods(ctx, reflLife)
		if err != nil {
			return false, "", err
		}
		rolled := len(pods) > 0
		for _, p := range pods {
			old := slices.ContainsFunc(podsBefore, func(o corev1.Pod) bool { return o.UID == p.UID })
			rolled = rolled && !old && podReady(&p) && p.Annotations["weft.aquaduct.dev/cert-fingerprint"] == fp
		}
		sans := slices.Sorted(slices.Values(cert.DNSNames))
		ok := slices.Equal(sans, []string{hostA, hostB}) && fp != fpBefore &&
			dep.Spec.Template.Annotations["weft.aquaduct.dev/cert-fingerprint"] == fp && rolled
		return ok, fmt.Sprintf("sans=%v new-fingerprint=%v notAfter %s->%s proxy-pods=%d rolled=%v", sans, fp != fpBefore,
			certBefore.NotAfter.Format(time.RFC3339), cert.NotAfter.Format(time.RFC3339), len(pods), rolled), nil
	}); err != nil {
		return err
	}
	if err := l.waitStatusFor(ctx, "b claimed but unrouted until its tunnel exists", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		return slices.Equal(r.Status.ReflectedHostnames, []string{hostA, hostB}) && slices.Equal(r.Status.UnroutedHostnames, []string{hostB}),
			fmt.Sprintf("reflected=%v unrouted=%v", r.Status.ReflectedHostnames, r.Status.UnroutedHostnames)
	}); err != nil {
		return err
	}
	if err := l.createTunnel(ctx, "b", registrySrc, "https://"+hostB+"/"); err != nil {
		return err
	}
	if err := l.waitStatusFor(ctx, "both hostnames routed and Ready", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		return len(r.Status.Routes) == 2 && len(r.Status.UnroutedHostnames) == 0 && condIs(r, "Ready", metav1.ConditionTrue, ""),
			fmt.Sprintf("routes=%v unrouted=%v", r.Status.Routes, r.Status.UnroutedHostnames)
	}); err != nil {
		return err
	}
	if err := l.clientPod(ctx, "new-host", "client", "--host="+hostB, "--expect-ip="+l.serviceIP); err != nil {
		return fmt.Errorf("new hostname: %w", err)
	}
	return l.waitNode(ctx, 2*time.Minute, "node agent maps both hostnames", []string{hostA, hostB})
}

func (l *lifecycleScenario) proxyDown(ctx context.Context) error {
	if err := l.setUnschedulable(ctx, true); err != nil {
		return err
	}
	pods, err := l.proxyPods(ctx, reflLife)
	if err != nil {
		return err
	}
	for _, p := range pods {
		if err := l.k.C.Delete(ctx, &p, client.GracePeriodSeconds(0)); err != nil {
			return err
		}
	}
	if err := l.waitStatusFor(ctx, "proxy unavailable, DNS withdrawn", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := l.hasCoreDNSKey(ctx, reflLife)
		ok := condIs(r, "ProxyAvailable", metav1.ConditionFalse, "Waiting") && condIs(r, "DNSPublished", metav1.ConditionFalse, "Withdrawn") &&
			condIs(r, "Ready", metav1.ConditionFalse, "") && !key
		return ok, fmt.Sprintf("coredns-key=%v", key)
	}); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "node agent withdrew its /etc/hosts entry", func() (bool, string, error) {
		line := nodeHostsLine(reflLife)
		agent, err := l.nodeAgentExists(ctx, reflLife)
		// The agent is still deployed; only its TLS check fails.
		return line == "" && agent && nodeTrustHasCA(reflLife), fmt.Sprintf("hosts=%q agent=%v", line, agent), err
	}); err != nil {
		return err
	}
	if err := l.setUnschedulable(ctx, false); err != nil {
		return err
	}
	if err := l.waitReady(ctx, 3*time.Minute); err != nil {
		return err
	}
	return l.waitNode(ctx, time.Minute, "node agent restored its /etc/hosts entry", []string{hostA, hostB})
}

func (l *lifecycleScenario) agentReasserts(ctx context.Context) error {
	if err := removeBlock(nodeHostsFile, hostsBegin(reflLife), hostsEnd(reflLife)); err != nil {
		return err
	}
	if err := removeBlock(nodeCABundle, caBegin(reflLife), caEnd(reflLife)); err != nil {
		return err
	}
	certsD := nodeCertsDir + "/" + hostA
	if err := os.RemoveAll(certsD); err != nil {
		return err
	}
	start := time.Now()
	// The agent re-checks every 15s.
	return e2ekit.WaitFor(ctx, 40*time.Second, "node agent re-asserted its node state", func() (bool, string, error) {
		line := nodeHostsLine(reflLife)
		ca := nodeTrustHasCA(reflLife)
		_, err := os.Stat(certsD + "/hosts.toml")
		ok := line == l.serviceIP+" "+hostA+" "+hostB && ca && err == nil
		return ok, fmt.Sprintf("hosts=%q trust-store=%v certs.d=%v after %s", line, ca, err == nil, time.Since(start).Round(time.Second)), nil
	})
}

func (l *lifecycleScenario) clusterDNSDisabled(ctx context.Context) error {
	if err := l.patchReflector(ctx, reflLife, func(s *weftv1alpha1.WeftLocalDNSReflectorSpec) { s.ClusterDNS.Disabled = true }); err != nil {
		return err
	}
	if err := l.waitStatusFor(ctx, "CoreDNS override removed, still Ready", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := l.hasCoreDNSKey(ctx, reflLife)
		ok := condIs(r, "DNSPublished", metav1.ConditionTrue, "Disabled") && condIs(r, "Ready", metav1.ConditionTrue, "") && !key
		return ok, fmt.Sprintf("coredns-key=%v", key)
	}); err != nil {
		return err
	}
	if err := l.clientPod(ctx, "dns-disabled", "client", "--host="+hostA, "--expect-unresolvable"); err != nil {
		return err
	}
	// The node half is independent of cluster DNS.
	return l.waitNode(ctx, time.Minute, "node agent entries kept", []string{hostA, hostB})
}

func (l *lifecycleScenario) nodeTrustDisabled(ctx context.Context) error {
	if err := l.patchReflector(ctx, reflLife, func(s *weftv1alpha1.WeftLocalDNSReflectorSpec) { s.NodeTrust.Disabled = true }); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "node agent and its node state removed", func() (bool, string, error) {
		agent, err := l.nodeAgentExists(ctx, reflLife)
		if err != nil {
			return false, "", err
		}
		line := nodeHostsLine(reflLife)
		bundle, _ := os.ReadFile(nodeCABundle)
		pristine, _ := os.ReadFile(pristineCABundle)
		restored := string(bundle) == string(pristine)
		var certs []string
		for _, h := range []string{hostA, hostB} {
			if _, err := os.Stat(nodeCertsDir + "/" + h); err == nil {
				certs = append(certs, h)
			}
		}
		return !agent && line == "" && restored && len(certs) == 0,
			fmt.Sprintf("agent=%v hosts=%q trust-store-restored=%v certs.d=%v", agent, line, restored, certs), nil
	})
}

// --- helpers -----------------------------------------------------------------

func (l *lifecycleScenario) waitStatusFor(ctx context.Context, what string, check func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string)) error {
	return l.waitStatus(ctx, reflLife, 3*time.Minute, what, check)
}

func (l *lifecycleScenario) waitReady(ctx context.Context, timeout time.Duration) error {
	return l.waitStatus(ctx, reflLife, timeout, "Ready with its CoreDNS override", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		l.serviceIP = r.Status.ServiceIP
		key := l.hasCoreDNSKey(ctx, reflLife)
		return condIs(r, "Ready", metav1.ConditionTrue, "") && key, fmt.Sprintf("coredns-key=%v", key)
	})
}

// waitNode waits for the agent's /etc/hosts entry mapping hosts, its node
// trust-store block and a certs.d entry per hostname.
func (l *lifecycleScenario) waitNode(ctx context.Context, timeout time.Duration, what string, hosts []string) error {
	want := l.serviceIP + " " + strings.Join(hosts, " ")
	return e2ekit.WaitFor(ctx, timeout, what, func() (bool, string, error) {
		line := nodeHostsLine(reflLife)
		ca := nodeTrustHasCA(reflLife)
		var missing []string
		for _, h := range hosts {
			if _, err := os.Stat(nodeCertsDir + "/" + h + "/hosts.toml"); err != nil {
				missing = append(missing, h)
			}
		}
		return line == want && ca && len(missing) == 0, fmt.Sprintf("hosts=%q want %q trust-store=%v certs.d-missing=%v", line, want, ca, missing), nil
	})
}

func (l *lifecycleScenario) setUnschedulable(ctx context.Context, on bool) error {
	nodes := &corev1.NodeList{}
	if err := l.k.C.List(ctx, nodes); err != nil {
		return err
	}
	for _, n := range nodes.Items {
		patch := client.MergeFrom(n.DeepCopy())
		n.Spec.Unschedulable = on
		if err := l.k.C.Patch(ctx, &n, patch); err != nil {
			return err
		}
	}
	return nil
}
