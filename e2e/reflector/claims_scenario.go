// claims_scenario.go — the "reflector-claims" scenario
// (//e2e/reflector:reflector_claims_test): what several reflectors and
// several kinds of tunnel do to each other in one cluster.
//
//   - routes: an https and an http tunnel are served (the http one on the
//     proxy's HTTP port, fetched over plain HTTP), while tunnels the proxy
//     cannot serve (tcp source, loopback source) and a hostname with no
//     tunnel are reported in status.unroutedHostnames and get no DNS; a
//     second tunnel to an already-served destination is dropped;
//   - trust gate: ignoreUntrustedPods publishes DNS despite a pod that
//     predates the reflector (ClientsTrustCA=True/Ignored); without it the
//     gate closes on exactly that pod, while pods opted out by label or
//     annotation and pods in kube-system never hold it;
//   - claims: an older allHostnames reflector loses every explicitly listed
//     hostname, a younger explicit reflector loses to the older one
//     (ProxyAvailable=False/NoRoutes), and deleting the winner hands the
//     hostname to the loser: its DNS key replaces the winner's and clients
//     reach it, while the allHostnames reflector picks up the hostnames
//     nobody lists any more.
package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	hostA     = "a.weft-e2e.test"
	hostPlain = "plain.weft-e2e.test"
	hostTCP   = "tcpsrc.weft-e2e.test"
	hostLoop  = "loop.weft-e2e.test"
	hostNone  = "nothere.weft-e2e.test"

	// zeta is the older explicit reflector, alpha the younger one: the
	// names sort the other way round so only age can decide.
	reflAll   = "everything"
	reflZeta  = "zeta"
	reflAlpha = "alpha"
)

type claimsScenario struct {
	*extras
	fps map[string]string
}

func claimsSteps(k *e2ekit.Kit) []e2ekit.Step {
	c := &claimsScenario{extras: newExtras(k), fps: map[string]string{}}
	k.Diagnostics = c.diagnostics
	return []e2ekit.Step{
		{ID: "claims.install", Label: "operator installed from the chart (no default reflector)", Fn: c.install},
		{ID: "claims.workloads", Label: "registry backend plus pods that predate any reflector: one plain, one opted out by label, one by annotation, one in kube-system", Fn: c.workloads},
		{ID: "claims.all-first", Label: "an allHostnames reflector created first claims nothing while no tunnel exists (NoRoutes)", Fn: c.allFirst},
		{ID: "claims.routes", Label: "https + http tunnels routed; tcp-source, loopback-source and tunnel-less hostnames unrouted; duplicate destination dropped", Fn: c.routes},
		{ID: "claims.ignore-untrusted", Label: "ignoreUntrustedPods: ClientsTrustCA=True/Ignored and DNS published despite the pre-existing pod", Fn: c.ignoreUntrusted},
		{ID: "claims.explicit-beats-all", Label: "the older allHostnames reflector reports every explicitly claimed hostname as conflicting and serves none", Fn: c.explicitBeatsAll},
		{ID: "claims.traffic", Label: "https and plain-http routes reachable through the proxy; an unrouted hostname does not resolve", Fn: c.traffic},
		{ID: "claims.trust-gate", Label: "without ignoreUntrustedPods the gate closes on the pre-existing pods only; opted-out and kube-system pods never hold it", Fn: c.trustGate},
		{ID: "claims.older-wins", Label: "a younger explicit reflector on the same hostname loses it (conflictingHostnames, NoRoutes, no DNS key)", Fn: c.olderWins},
		{ID: "claims.handover", Label: "deleting the winner moves the hostname and its DNS key to the loser and clients reach it; allHostnames picks up released hostnames", Fn: c.handover},
	}
}

func (c *claimsScenario) workloads(ctx context.Context) error {
	if err := c.legacy.deployBackend(ctx); err != nil {
		return err
	}
	for _, p := range []struct {
		ns, name            string
		labels, annotations map[string]string
	}{
		{clientNS, "legacy", nil, nil},
		{clientNS, "optout-label", map[string]string{optOutKey: "false"}, nil},
		{clientNS, "optout-annotation", nil, map[string]string{optOutKey: "false"}},
		{"kube-system", "system-legacy", nil, nil},
	} {
		if err := c.sleeper(ctx, p.ns, p.name, p.labels, p.annotations); err != nil {
			return err
		}
	}
	return nil
}

func (c *claimsScenario) allFirst(ctx context.Context) error {
	if err := c.createReflector(ctx, reflAll, weftv1alpha1.WeftLocalDNSReflectorSpec{AllHostnames: true}); err != nil {
		return err
	}
	fp, err := c.waitCA(ctx, reflAll)
	if err != nil {
		return err
	}
	c.fps[reflAll] = fp
	return c.waitStatus(ctx, reflAll, 2*time.Minute, "claims nothing, NoRoutes", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		return len(r.Status.ReflectedHostnames) == 0 && condIs(r, "ProxyAvailable", metav1.ConditionFalse, "NoRoutes"),
			fmt.Sprintf("reflected=%v", r.Status.ReflectedHostnames)
	})
}

func (c *claimsScenario) routes(ctx context.Context) error {
	// zeta first, so the allHostnames reflector never claims its hostnames.
	if err := c.createReflector(ctx, reflZeta, weftv1alpha1.WeftLocalDNSReflectorSpec{
		Hostnames:           []string{hostA, hostPlain, hostTCP, hostLoop, hostNone},
		IgnoreUntrustedPods: true,
	}); err != nil {
		return err
	}
	for _, t := range []struct{ name, src, dst string }{
		{"a", registrySrc, "https://" + hostA + "/"},
		// Same destination as "a"; sorts after it, so it is the one dropped.
		{"a-dup", registrySrc, "https://" + hostA + "/"},
		{"plain", registrySrc, "http://" + hostPlain + "/"},
		{"tcpsrc", "tcp://registry." + backendNS + ".svc:5000", "https://" + hostTCP + "/"},
		{"loop", "http://127.0.0.1:5000", "https://" + hostLoop + "/"},
	} {
		if err := c.createTunnel(ctx, t.name, t.src, t.dst); err != nil {
			return err
		}
	}
	fp, err := c.waitCA(ctx, reflZeta)
	if err != nil {
		return err
	}
	c.fps[reflZeta] = fp
	want := []weftv1alpha1.ReflectedRoute{
		{Hostname: hostA, Source: registrySrc, Destination: "https://" + hostA + ":8443/", Tunnel: backendNS + "/a"},
		{Hostname: hostPlain, Source: registrySrc, Destination: "http://" + hostPlain + ":8080/", Tunnel: backendNS + "/plain"},
	}
	slices.SortFunc(want, func(a, b weftv1alpha1.ReflectedRoute) int { return strings.Compare(a.Destination, b.Destination) })
	wantUnrouted := []string{hostLoop, hostNone, hostTCP}
	return c.waitStatus(ctx, reflZeta, 3*time.Minute, "two routes, three unrouted, proxy available", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		ok := slices.Equal(r.Status.Routes, want) && slices.Equal(r.Status.UnroutedHostnames, wantUnrouted) &&
			condIs(r, "ProxyAvailable", metav1.ConditionTrue, "Available")
		return ok, fmt.Sprintf("routes=%v unrouted=%v", r.Status.Routes, r.Status.UnroutedHostnames)
	})
}

func (c *claimsScenario) ignoreUntrusted(ctx context.Context) error {
	return c.waitStatus(ctx, reflZeta, 3*time.Minute, "Ignored gate, DNS published, Ready", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := c.hasCoreDNSKey(ctx, reflZeta)
		ok := condIs(r, "ClientsTrustCA", metav1.ConditionTrue, "Ignored") &&
			r.Status.UntrustedPods > 0 && slices.Contains(r.Status.UntrustedPodSample, clientNS+"/legacy") &&
			condIs(r, "DNSPublished", metav1.ConditionTrue, "Published") && condIs(r, "Ready", metav1.ConditionTrue, "") && key
		return ok, fmt.Sprintf("untrusted=%d sample=%v coredns-key=%v", r.Status.UntrustedPods, r.Status.UntrustedPodSample, key)
	})
}

func (c *claimsScenario) explicitBeatsAll(ctx context.Context) error {
	// Every tunnel hostname is listed by zeta, so the (older) allHostnames
	// reflector must give up all of them.
	want := []string{hostA, hostLoop, hostPlain, hostTCP}
	return c.waitStatus(ctx, reflAll, 2*time.Minute, "every tunnel hostname conflicting", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := c.hasCoreDNSKey(ctx, reflAll)
		ok := slices.Equal(r.Status.ConflictingHostnames, want) && len(r.Status.ReflectedHostnames) == 0 &&
			condIs(r, "ProxyAvailable", metav1.ConditionFalse, "NoRoutes") && !key
		return ok, fmt.Sprintf("conflicting=%v reflected=%v coredns-key=%v", r.Status.ConflictingHostnames, r.Status.ReflectedHostnames, key)
	})
}

func (c *claimsScenario) traffic(ctx context.Context) error {
	r, err := c.reflector(ctx, reflZeta)
	if err != nil {
		return err
	}
	ip := r.Status.ServiceIP
	if err := c.clientPod(ctx, "https-a", "client", "--host="+hostA, "--expect-ip="+ip); err != nil {
		return fmt.Errorf("https route: %w", err)
	}
	if err := c.clientPod(ctx, "http-plain", "fetch", "--url=http://"+hostPlain+"/v2/", "--expect-ip="+ip); err != nil {
		return fmt.Errorf("http route: %w", err)
	}
	if err := c.clientPod(ctx, "unrouted", "client", "--host="+hostTCP, "--expect-unresolvable"); err != nil {
		return fmt.Errorf("unrouted hostname: %w", err)
	}
	return nil
}

func (c *claimsScenario) trustGate(ctx context.Context) error {
	if err := c.patchReflector(ctx, reflZeta, func(s *weftv1alpha1.WeftLocalDNSReflectorSpec) { s.IgnoreUntrustedPods = false }); err != nil {
		return err
	}
	exempt := []string{clientNS + "/optout-label", clientNS + "/optout-annotation", "kube-system/system-legacy"}
	if err := c.waitStatus(ctx, reflZeta, 2*time.Minute, "gate closed on the pre-existing pods only, DNS and node agent withdrawn", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := c.hasCoreDNSKey(ctx, reflZeta)
		agent, _ := c.nodeAgentExists(ctx, reflZeta)
		sample := r.Status.UntrustedPodSample
		registry := slices.ContainsFunc(sample, func(p string) bool { return strings.HasPrefix(p, backendNS+"/registry-") })
		leaked := slices.ContainsFunc(exempt, func(p string) bool { return slices.Contains(sample, p) })
		ok := condIs(r, "ClientsTrustCA", metav1.ConditionFalse, "PodsNeedRestart") && r.Status.UntrustedPods == 2 &&
			slices.Contains(sample, clientNS+"/legacy") && registry && !leaked &&
			condIs(r, "DNSPublished", metav1.ConditionFalse, "WaitingForTrust") && !key && !agent
		return ok, fmt.Sprintf("untrusted=%d sample=%v coredns-key=%v node-agent=%v", r.Status.UntrustedPods, sample, key, agent)
	}); err != nil {
		return err
	}
	if err := c.deletePod(ctx, clientNS, "legacy"); err != nil {
		return err
	}
	if err := c.restartRegistry(ctx, c.fps[reflAll], c.fps[reflZeta]); err != nil {
		return err
	}
	return c.waitStatus(ctx, reflZeta, 3*time.Minute, "gate open with only exempt pods untrusted, DNS published", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := c.hasCoreDNSKey(ctx, reflZeta)
		running := c.allRunning(ctx, exempt)
		ok := condIs(r, "ClientsTrustCA", metav1.ConditionTrue, "AllPodsTrust") && r.Status.UntrustedPods == 0 && running &&
			condIs(r, "DNSPublished", metav1.ConditionTrue, "Published") && key
		return ok, fmt.Sprintf("untrusted=%d exempt-pods-running=%v coredns-key=%v", r.Status.UntrustedPods, running, key)
	})
}

func (c *claimsScenario) olderWins(ctx context.Context) error {
	if err := c.createReflector(ctx, reflAlpha, weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{hostA}}); err != nil {
		return err
	}
	fp, err := c.waitCA(ctx, reflAlpha)
	if err != nil {
		return err
	}
	c.fps[reflAlpha] = fp
	if err := c.waitStatus(ctx, reflAlpha, 2*time.Minute, "loses "+hostA+" to the older reflector", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		key := c.hasCoreDNSKey(ctx, reflAlpha)
		ok := slices.Equal(r.Status.ConflictingHostnames, []string{hostA}) && len(r.Status.ReflectedHostnames) == 0 &&
			len(r.Status.Routes) == 0 && condIs(r, "ProxyAvailable", metav1.ConditionFalse, "NoRoutes") && !key
		return ok, fmt.Sprintf("conflicting=%v reflected=%v coredns-key=%v", r.Status.ConflictingHostnames, r.Status.ReflectedHostnames, key)
	}); err != nil {
		return err
	}
	zeta, err := c.reflector(ctx, reflZeta)
	if err != nil {
		return err
	}
	if !slices.Contains(zeta.Status.ReflectedHostnames, hostA) || !c.hasCoreDNSKey(ctx, reflZeta) {
		return fmt.Errorf("older reflector lost %s: reflected=%v keys=%v", hostA, zeta.Status.ReflectedHostnames, c.coreDNSKeys(ctx))
	}
	// So alpha's gate can open once it wins, the backend must trust it.
	return c.restartRegistry(ctx, c.fps[reflAll], c.fps[reflZeta], c.fps[reflAlpha])
}

func (c *claimsScenario) handover(ctx context.Context) error {
	zeta, err := c.reflector(ctx, reflZeta)
	if err != nil {
		return err
	}
	if err := c.k.C.Delete(ctx, zeta); err != nil {
		return err
	}
	if err := c.waitStatus(ctx, reflAlpha, 3*time.Minute, "takes over "+hostA, func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		keys := c.coreDNSKeys(ctx)
		ok := slices.Equal(r.Status.ReflectedHostnames, []string{hostA}) && len(r.Status.ConflictingHostnames) == 0 && len(r.Status.Routes) == 1 &&
			condIs(r, "Ready", metav1.ConditionTrue, "") && slices.Contains(keys, reflAlpha) && !slices.Contains(keys, reflZeta)
		return ok, fmt.Sprintf("reflected=%v conflicting=%v coredns-keys=%v", r.Status.ReflectedHostnames, r.Status.ConflictingHostnames, keys)
	}); err != nil {
		return err
	}
	alpha, err := c.reflector(ctx, reflAlpha)
	if err != nil {
		return err
	}
	if alpha.Status.ServiceIP == zeta.Status.ServiceIP {
		return fmt.Errorf("alpha and zeta share service IP %s", alpha.Status.ServiceIP)
	}
	if err := c.clientPod(ctx, "handover", "client", "--host="+hostA, "--expect-ip="+alpha.Status.ServiceIP); err != nil {
		return fmt.Errorf("client after handover: %w", err)
	}
	// Nobody lists the other tunnel hostnames now: allHostnames takes them.
	if err := c.waitStatus(ctx, reflAll, 3*time.Minute, "picks up the released hostnames", func(r *weftv1alpha1.WeftLocalDNSReflector) (bool, string) {
		ok := slices.Equal(r.Status.ReflectedHostnames, []string{hostLoop, hostPlain, hostTCP}) &&
			slices.Equal(r.Status.ConflictingHostnames, []string{hostA}) && condIs(r, "Ready", metav1.ConditionTrue, "") && c.hasCoreDNSKey(ctx, reflAll)
		return ok, fmt.Sprintf("reflected=%v conflicting=%v", r.Status.ReflectedHostnames, r.Status.ConflictingHostnames)
	}); err != nil {
		return err
	}
	all, err := c.reflector(ctx, reflAll)
	if err != nil {
		return err
	}
	return c.clientPod(ctx, "all-plain", "fetch", "--url=http://"+hostPlain+"/v2/", "--expect-ip="+all.Status.ServiceIP)
}

// allRunning reports whether every ns/name pod is Running.
func (c *claimsScenario) allRunning(ctx context.Context, pods []string) bool {
	for _, p := range pods {
		ns, name, _ := strings.Cut(p, "/")
		pod := &corev1.Pod{}
		if err := c.k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, pod); err != nil || pod.Status.Phase != corev1.PodRunning {
			return false
		}
	}
	return true
}
