// agent_test.go — unit tests for the node agent: the tagged /etc/hosts block
// edit, containerd certs.d ownership rules, and the health-gated Tick/Cleanup
// cycle against a temp directory standing in for the node.
package nodeagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const baseHosts = "127.0.0.1 localhost\n192.168.86.88 mnemosyne\n"

func TestWithHostsBlock(t *testing.T) {
	added := withHostsBlock(baseHosts, "lab0", "10.43.0.5", []string{"a.lab0.us", "b.lab0.us"})
	t.Logf("added:\n%s", added)
	if !strings.HasPrefix(added, baseHosts) || !strings.Contains(added, "10.43.0.5 a.lab0.us b.lab0.us\n") {
		t.Fatalf("block not appended correctly")
	}
	if again := withHostsBlock(added, "lab0", "10.43.0.5", []string{"a.lab0.us", "b.lab0.us"}); again != added {
		t.Errorf("not idempotent:\n%s", again)
	}

	// Replaced in place, other reflectors' and users' lines untouched.
	withOther := added + "10.1.1.1 manual.example\n"
	withOther = withHostsBlock(withOther, "other", "10.43.0.9", []string{"c.lab0.us"})
	changed := withHostsBlock(withOther, "lab0", "10.43.0.6", []string{"a.lab0.us"})
	t.Logf("changed:\n%s", changed)
	if strings.Contains(changed, "10.43.0.5") || !strings.Contains(changed, "10.43.0.6 a.lab0.us\n") {
		t.Errorf("block not replaced")
	}
	if !strings.Contains(changed, "10.1.1.1 manual.example") || !strings.Contains(changed, "10.43.0.9 c.lab0.us") {
		t.Errorf("foreign lines lost")
	}
	if strings.Index(changed, "10.43.0.6") > strings.Index(changed, "manual.example") {
		t.Errorf("block moved instead of being replaced in place")
	}

	if removed := withHostsBlock(added, "lab0", "", nil); removed != baseHosts {
		t.Errorf("removal left %q", removed)
	}

	// A BEGIN without END must not swallow the rest of the file.
	broken := blockBegin("lab0") + "\n" + baseHosts
	repaired := withHostsBlock(broken, "lab0", "", nil)
	if repaired != baseHosts {
		t.Errorf("unterminated block handling dropped lines: %q", repaired)
	}
}

func TestCertsDOwnership(t *testing.T) {
	dir := t.TempDir()
	ca := []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n")

	changed, err := writeCertsD(dir, "lab0", "gitea.lab0.us", ca)
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	toml, _ := os.ReadFile(filepath.Join(dir, "gitea.lab0.us", hostsTOML))
	t.Logf("hosts.toml:\n%s", toml)
	wantCA := filepath.Join(dir, "gitea.lab0.us", caFileName)
	if !strings.Contains(string(toml), `server = "https://gitea.lab0.us"`) || !strings.Contains(string(toml), `ca = "`+wantCA+`"`) {
		t.Errorf("unexpected hosts.toml")
	}
	if changed, _ := writeCertsD(dir, "lab0", "gitea.lab0.us", ca); changed {
		t.Errorf("rewrite of identical content reported a change")
	}

	// Someone else's hosts.toml is never overwritten or removed.
	foreign := filepath.Join(dir, "registry.example")
	os.MkdirAll(foreign, 0o700)
	os.WriteFile(filepath.Join(foreign, hostsTOML), []byte("server = \"http://registry.example\"\n"), 0o644)
	if _, err := writeCertsD(dir, "lab0", "registry.example", ca); err == nil {
		t.Errorf("expected refusal to overwrite a foreign hosts.toml")
	}
	if removed, _ := removeCertsD(dir, "lab0", "registry.example"); removed {
		t.Errorf("removed a foreign directory")
	}
	// Nor is another reflector's.
	if removed, _ := removeCertsD(dir, "other", "gitea.lab0.us"); removed {
		t.Errorf("removed another reflector's directory")
	}

	if got := ownedHostnames(dir, "lab0"); len(got) != 1 || got[0] != "gitea.lab0.us" {
		t.Errorf("owned = %v", got)
	}
	if removed, err := removeCertsD(dir, "lab0", "gitea.lab0.us"); !removed || err != nil {
		t.Errorf("own removal: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(foreign, hostsTOML)); err != nil {
		t.Errorf("foreign file disturbed: %v", err)
	}
}

func TestAgentTickAndCleanup(t *testing.T) {
	dir := t.TempDir()
	hostsFile := filepath.Join(dir, "hosts")
	certsDir := filepath.Join(dir, "certs.d")
	caFile := filepath.Join(dir, "ca.crt")
	os.WriteFile(hostsFile, []byte(baseHosts), 0o644)
	os.MkdirAll(certsDir, 0o700)
	// Any parseable cert works; the dialer is faked.
	os.WriteFile(caFile, testCAPEM(t), 0o644)

	hostBundle := filepath.Join(dir, "ca-certificates.crt")
	const distroRoots = "-----BEGIN CERTIFICATE-----\nZGlzdHJv\n-----END CERTIFICATE-----\n"
	os.WriteFile(hostBundle, []byte(distroRoots), 0o644)
	cfg := Config{Reflector: "lab0", ServiceIP: "10.43.0.5", Hostnames: []string{"gitea.lab0.us"}, CAFile: caFile, HostsFile: hostsFile, CertsDir: certsDir,
		HostTrustFiles: []string{hostBundle, filepath.Join(dir, "missing-bundle.crt")}}
	agent := NewAgent(cfg, zap.New(zap.UseDevMode(true)))
	healthy := true
	agent.dialer = func(_ context.Context, addr, sni string, _ *x509.CertPool) error {
		t.Logf("dial %s sni=%s healthy=%v", addr, sni, healthy)
		if addr != "10.43.0.5:443" || (sni != "gitea.lab0.us" && sni != "other.lab0.us") {
			t.Errorf("unexpected dial %s %s", addr, sni)
		}
		if !healthy {
			return errors.New("connection refused")
		}
		return nil
	}
	read := func() string { b, _ := os.ReadFile(hostsFile); return string(b) }

	agent.Tick(context.Background())
	if !strings.Contains(read(), "10.43.0.5 gitea.lab0.us") {
		t.Fatalf("healthy tick did not add hosts entry:\n%s", read())
	}
	if _, err := os.Stat(filepath.Join(certsDir, "gitea.lab0.us", hostsTOML)); err != nil {
		t.Fatalf("certs.d not written: %v", err)
	}
	ca, _ := os.ReadFile(caFile)
	bundle, _ := os.ReadFile(hostBundle)
	t.Logf("host bundle after tick: %d bytes", len(bundle))
	if !strings.HasPrefix(string(bundle), distroRoots) || !strings.Contains(string(bundle), strings.TrimSpace(string(ca))) {
		t.Fatalf("host CA bundle not extended correctly")
	}
	if _, err := os.Stat(filepath.Join(dir, "missing-bundle.crt")); !os.IsNotExist(err) {
		t.Errorf("a missing host bundle must not be created")
	}

	healthy = false
	agent.Tick(context.Background())
	if read() != baseHosts {
		t.Fatalf("unhealthy tick must withdraw the hosts entry:\n%s", read())
	}
	if _, err := os.Stat(filepath.Join(certsDir, "gitea.lab0.us", hostsTOML)); err != nil {
		t.Fatalf("certs.d trust should stay while unhealthy: %v", err)
	}

	// A hostname dropped from the config is pruned from certs.d.
	healthy = true
	agent.Tick(context.Background())
	agent.cfg.Hostnames = []string{"other.lab0.us"}
	agent.Tick(context.Background())
	if _, err := os.Stat(filepath.Join(certsDir, "gitea.lab0.us")); !os.IsNotExist(err) {
		t.Errorf("stale hostname not pruned: %v", err)
	}

	if err := agent.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if read() != baseHosts {
		t.Errorf("cleanup left hosts entry:\n%s", read())
	}
	if b, _ := os.ReadFile(hostBundle); string(b) != distroRoots {
		t.Errorf("cleanup left the host CA block:\n%s", b)
	}
	if entries, _ := os.ReadDir(certsDir); len(entries) != 0 {
		t.Errorf("cleanup left certs.d entries: %v", entries)
	}
}

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"--reflector=lab0", "--service-ip=10.43.0.5", "--hostnames=B.lab0.us, a.lab0.us", "--ca-file=/ca", "--certs-dir=/c"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Hostnames, ",") != "a.lab0.us,b.lab0.us" {
		t.Errorf("hostnames = %v", cfg.Hostnames)
	}
	if _, err := parseFlags([]string{"--reflector=lab0", "--service-ip=nope", "--hostnames=a", "--ca-file=/ca", "--certs-dir=/c"}); err == nil {
		t.Errorf("bad IP accepted")
	}
}

// testCAPEM returns a throwaway self-signed certificate so the CA pool
// parses; the dialer is faked, so nothing ever verifies against it.
func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
