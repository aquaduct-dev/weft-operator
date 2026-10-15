// pki_test.go — unit tests for CA/serving-certificate issue, verification
// and the renewal decision.
package pki

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"
)

func TestCertificates(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	caPEM, caKey, err := NewCA("test", now)
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{"gitea.lab0.us", "Bazel-Cache.lab0.us"}
	leafPEM, leafKey, err := IssueLeaf(caPEM, caKey, hosts, now)
	if err != nil {
		t.Fatal(err)
	}

	// The pair must load as a TLS keypair and verify against the CA.
	if _, err := tls.X509KeyPair(leafPEM, leafKey); err != nil {
		t.Fatalf("leaf keypair: %v", err)
	}
	leaf, _ := ParseCert(leafPEM)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	for _, h := range []string{"gitea.lab0.us", "bazel-cache.lab0.us"} {
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: h, Roots: roots, CurrentTime: now}); err != nil {
			t.Errorf("verify %s: %v", h, err)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "evil.lab0.us", Roots: roots, CurrentTime: now}); err == nil {
		t.Errorf("leaf must not verify for an unlisted host")
	}

	cases := []struct {
		name    string
		leaf    []byte
		ca      []byte
		hosts   []string
		at      time.Time
		renewal bool
	}{
		{"current", leafPEM, caPEM, hosts, now, false},
		{"order and case do not matter", leafPEM, caPEM, []string{"bazel-cache.lab0.us", "GITEA.lab0.us"}, now, false},
		{"missing", nil, caPEM, hosts, now, true},
		{"host added", leafPEM, caPEM, append(hosts, "new.lab0.us"), now, true},
		{"inside renewal window", leafPEM, caPEM, hosts, now.Add(LeafValidity - LeafRenewBefore + time.Hour), true},
		{"other CA", leafPEM, mustCA(t, now), hosts, now, true},
	}
	for _, c := range cases {
		reason := RenewalReason(c.leaf, c.ca, c.hosts, c.at)
		t.Logf("%s: reason=%q", c.name, reason)
		if (reason != "") != c.renewal {
			t.Errorf("%s: renewal=%v, want %v", c.name, reason != "", c.renewal)
		}
	}

	if _, _, err := IssueLeaf(caPEM, caKey, nil, now); err == nil {
		t.Errorf("issuing with no hostnames must fail")
	}
	if Fingerprint(leafPEM) == Fingerprint(caPEM) || len(Fingerprint(leafPEM)) != 16 {
		t.Errorf("fingerprint not distinct/short")
	}
}

func mustCA(t *testing.T, now time.Time) []byte {
	t.Helper()
	c, _, err := NewCA("other", now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCountCerts(t *testing.T) {
	a, _, _ := NewCA("a", time.Now())
	b, _, _ := NewCA("b", time.Now())
	if n := CountCerts(append(append([]byte("junk\n"), a...), b...)); n != 2 {
		t.Errorf("CountCerts = %d, want 2", n)
	}
}
