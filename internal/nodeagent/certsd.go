// certsd.go — containerd registry trust for reflected hostnames. For each
// hostname the agent writes <certs.d>/<hostname>/hosts.toml naming the
// reflector CA, so containerd accepts the in-cluster proxy's certificate.
// containerd adds that CA to the system roots rather than replacing them, so
// the file is harmless when /etc/hosts still points at the public endpoint.
// containerd reads certs.d on every pull; no restart is needed. The agent
// only ever touches directories carrying its own marker, never a hosts.toml
// someone else wrote.
package nodeagent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	hostsTOML  = "hosts.toml"
	caFileName = "weft-local-ca.crt"
)

func certsDMarker(reflector string) string {
	return fmt.Sprintf("# Managed by weft-operator WeftLocalDNSReflector %q. Do not edit.", reflector)
}

// renderHostsTOML is the containerd hosts.toml for one hostname. Top-level
// keys configure the `server` endpoint itself.
func renderHostsTOML(reflector, hostname, caPath string) []byte {
	return fmt.Appendf(nil, "%s\nserver = %q\nca = %q\n", certsDMarker(reflector), "https://"+hostname, caPath)
}

// ownedBy reports whether dir's hosts.toml exists and carries our marker.
// A missing file is "not owned" but also not foreign, see writeCertsD.
func ownedBy(dir, reflector string) (owned, exists bool) {
	data, err := os.ReadFile(filepath.Join(dir, hostsTOML))
	if err != nil {
		return false, !os.IsNotExist(err)
	}
	return strings.HasPrefix(string(data), certsDMarker(reflector)), true
}

// writeCertsD makes certsDir/<hostname> trust caPEM. It returns changed=true
// when it wrote anything, and an error (without writing) when the directory
// already holds someone else's hosts.toml.
func writeCertsD(certsDir, reflector, hostname string, caPEM []byte) (changed bool, err error) {
	dir := filepath.Join(certsDir, hostname)
	if owned, exists := ownedBy(dir, reflector); exists && !owned {
		return false, fmt.Errorf("%s already has a hosts.toml not managed by this reflector; leaving it alone", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("create %s: %w", dir, err)
	}
	caPath := filepath.Join(dir, caFileName)
	// CA first: hosts.toml must never name a CA file that is not there.
	c1, err := writeIfChanged(caPath, caPEM, 0o644)
	if err != nil {
		return false, err
	}
	c2, err := writeIfChanged(filepath.Join(dir, hostsTOML), renderHostsTOML(reflector, hostname, caPath), 0o644)
	if err != nil {
		return c1, err
	}
	return c1 || c2, nil
}

// removeCertsD deletes certsDir/<hostname> if, and only if, we own it.
func removeCertsD(certsDir, reflector, hostname string) (bool, error) {
	dir := filepath.Join(certsDir, hostname)
	if owned, _ := ownedBy(dir, reflector); !owned {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("remove %s: %w", dir, err)
	}
	return true, nil
}

// ownedHostnames lists the hostnames under certsDir this reflector manages,
// so hostnames dropped from the spec get cleaned up.
func ownedHostnames(certsDir, reflector string) []string {
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if owned, _ := ownedBy(filepath.Join(certsDir, e.Name()), reflector); owned {
			out = append(out, e.Name())
		}
	}
	return out
}

// writeIfChanged atomically replaces path with data when the content differs.
// The temp file lives in the same directory so the rename never crosses a
// filesystem (the directory is a hostPath mount).
func writeIfChanged(path string, data []byte, mode os.FileMode) (bool, error) {
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return false, fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("rename into %s: %w", path, err)
	}
	return true, nil
}
