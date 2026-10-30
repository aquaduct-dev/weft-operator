// pure_test.go — unit tests for the reflector's pure helpers: route
// derivation from WeftTunnels and the CoreDNS server-block renderer (whose output CoreDNS must be able to load).
package weftlocaldnsreflector

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

func tunnel(ns, name, src, dst string) weftv1alpha1.WeftTunnel {
	return weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: src, DstURL: dst},
	}
}

func TestComputeRoutes(t *testing.T) {
	tunnels := []weftv1alpha1.WeftTunnel{
		tunnel("gitea", "gw-b", "http://gitea-http.gitea.svc:3000", "https://gitea.lab0.us/"),
		// Same destination as gw-b but sorts first, so it wins.
		tunnel("gitea", "gw-a", "http://other.gitea.svc:3000", "https://gitea.lab0.us/"),
		tunnel("gitea", "gw-api", "http://api.gitea.svc:8080#header=X-A:1", "https://gitea.lab0.us/api"),
		tunnel("gitea", "gw-plain", "http://gitea-http.gitea.svc:3000", "http://gitea.lab0.us"),
		tunnel("other", "unrelated", "http://x.other.svc:80", "https://example.com/"),
		tunnel("other", "tcp", "tcp://x.other.svc:22", "tcp://gitea.lab0.us:22"),
		tunnel("other", "loop", "http://localhost:8080", "https://gitea.lab0.us/loop"),
	}
	set := computeRoutes([]string{"gitea.lab0.us", "missing.lab0.us"}, tunnels)
	for _, r := range set.Routes {
		t.Logf("route %s -> %s (%s)", r.Source, r.Destination, r.Tunnel)
	}
	for k, v := range set.Skipped {
		t.Logf("skipped %s: %s", k, v)
	}

	want := map[string]string{
		"https://gitea.lab0.us:8443/":    "gitea/gw-a",
		"https://gitea.lab0.us:8443/api": "gitea/gw-api",
		"http://gitea.lab0.us:8080/":     "gitea/gw-plain",
	}
	if len(set.Routes) != len(want) {
		t.Fatalf("got %d routes, want %d", len(set.Routes), len(want))
	}
	for _, r := range set.Routes {
		if want[r.Destination] != r.Tunnel {
			t.Errorf("destination %s from %s, want %s", r.Destination, r.Tunnel, want[r.Destination])
		}
		if r.Destination == "https://gitea.lab0.us:8443/api" && r.Source != "http://api.gitea.svc:8080#header=X-A:1" {
			t.Errorf("matcher fragment lost from source: %s", r.Source)
		}
	}
	for _, skipped := range []string{"gitea/gw-b", "other/tcp", "other/loop"} {
		if _, ok := set.Skipped[skipped]; !ok {
			t.Errorf("expected %s to be skipped", skipped)
		}
	}
	if _, ok := set.Skipped["other/unrelated"]; ok {
		t.Errorf("tunnel for an unrequested host should be ignored, not skipped")
	}
	if strings.Join(set.Unrouted, ",") != "missing.lab0.us" {
		t.Errorf("unrouted = %v", set.Unrouted)
	}
	if got := strings.Join(set.RoutedHostnames(), ","); got != "gitea.lab0.us" {
		t.Errorf("routed hostnames = %s", got)
	}
	if !set.HasScheme("https") || !set.HasScheme("http") {
		t.Errorf("expected both schemes")
	}

	args := proxyArgs(set.Routes, "/c", "/k")
	t.Logf("proxy args: %v", args)
	if args[0] != "proxy" || args[1] != "--tls-cert=/c" || args[2] != "--tls-key=/k" || len(args) != 3+2*len(set.Routes) {
		t.Errorf("unexpected proxy args %v", args)
	}
}

func TestRenderCoreDNS(t *testing.T) {
	out, err := renderCoreDNS("lab0", "10.43.12.34", []string{"gitea.lab0.us", "bazel-cache.lab0.us"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rendered:\n%s", out)
	for _, want := range []string{
		"bazel-cache.lab0.us:53 {",
		"gitea.lab0.us:53 {",
		"        10.43.12.34 gitea.lab0.us\n",
		"        fallthrough\n",
		"    forward . /etc/resolv.conf\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Errorf("unbalanced braces")
	}
	if strings.Index(out, "bazel-cache") > strings.Index(out, "gitea.lab0.us:53") {
		t.Errorf("blocks not sorted")
	}

	for _, bad := range []struct {
		ip    string
		hosts []string
	}{
		{"not-an-ip", []string{"gitea.lab0.us"}},
		{"10.0.0.1", nil},
		{"10.0.0.1", []string{"gitea.lab0.us {\n}"}},
		{"10.0.0.1", []string{"*.lab0.us"}},
		{"10.0.0.1", []string{"localhost"}},
		{"10.0.0.1", []string{"10.0.0.2"}},
	} {
		if _, err := renderCoreDNS("lab0", bad.ip, bad.hosts); err == nil {
			t.Errorf("render(%q, %q) should fail", bad.ip, bad.hosts)
		}
	}
}

func reflector(name string, age int, all bool, hosts ...string) weftv1alpha1.WeftLocalDNSReflector {
	return weftv1alpha1.WeftLocalDNSReflector{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, age, 0, 0, time.UTC))},
		Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{AllHostnames: all, Hostnames: hosts},
	}
}

func TestResolveClaims(t *testing.T) {
	tunnels := []weftv1alpha1.WeftTunnel{
		tunnel("a", "t1", "http://x.a.svc:80", "https://gitea.lab0.us/"),
		tunnel("a", "t2", "http://x.a.svc:80", "https://Authz.lab0.us/api"),
		tunnel("a", "t3", "http://x.a.svc:80", "http://plain.lab0.us"),
		tunnel("a", "t4", "tcp://x.a.svc:22", "tcp://ssh.lab0.us:22"),
		tunnel("a", "t5", "http://x.a.svc:80", "https://10.0.0.1/"),
	}
	deleting := reflector("gone", 0, false, "plain.lab0.us")
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	def := reflector("cluster-default", 5, true)
	lab0 := reflector("lab0", 3, false, "gitea.lab0.us")
	late := reflector("late", 9, false, "gitea.lab0.us", "other.lab0.us")
	secondAll := reflector("second-all", 7, true)
	all := []weftv1alpha1.WeftLocalDNSReflector{def, lab0, late, secondAll, deleting}

	check := func(self weftv1alpha1.WeftLocalDNSReflector, wantClaimed, wantConflict string) {
		t.Helper()
		claimed, conflicting := resolveClaims(&self, all, tunnels)
		t.Logf("%s: claimed=%v conflicting=%v", self.Name, claimed, conflicting)
		if strings.Join(claimed, ",") != wantClaimed || strings.Join(conflicting, ",") != wantConflict {
			t.Errorf("%s: claimed=%v conflicting=%v, want %q / %q", self.Name, claimed, conflicting, wantClaimed, wantConflict)
		}
	}
	// allHostnames takes every valid http(s) tunnel host except explicit
	// claims; the deleting reflector's claim no longer counts; tcp and IP
	// hosts are skipped.
	check(def, "authz.lab0.us,plain.lab0.us", "gitea.lab0.us")
	// The oldest explicit claim wins.
	check(lab0, "gitea.lab0.us", "")
	check(late, "other.lab0.us", "gitea.lab0.us")
	// A younger allHostnames reflector yields everything.
	check(secondAll, "", "authz.lab0.us,gitea.lab0.us,plain.lab0.us")

	if names := certNames(&secondAll, nil); len(names) != 1 || !strings.HasSuffix(names[0], placeholderCertSuffix) {
		t.Errorf("empty claim set needs a placeholder SAN, got %v", names)
	}
}
