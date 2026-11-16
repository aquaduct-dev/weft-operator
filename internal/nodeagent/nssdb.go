// nssdb.go — the reflector CA in the node's NSS trust stores. Chrome and
// Firefox on Linux ignore the distro CA bundle: they trust only their
// built-in roots plus what a per-user NSS database (~/.pki/nssdb, each
// Firefox profile) marks trusted. On a node someone also uses as a
// workstation, /etc/hosts sends the browser to the proxy, so without this its
// certificate is rejected as from an unknown issuer.
//
// NSS signs every trust record (an HMAC kept in key4.db, even when the
// database has no password), so the records are written with NSS's own
// certutil rather than by editing the SQLite files. certutil runs as the
// database's owner, so it never leaves root-owned files in a home directory.
package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// nssDBPatterns are where browsers keep their NSS databases, relative to a
// home directory. A match counts only if it holds a cert9.db (the SQLite
// format; the legacy cert8.db is not written).
var nssDBPatterns = []string{
	".pki/nssdb",                       // Chrome, Chromium and other NSS users
	"snap/chromium/current/.pki/nssdb", // Chromium snap
	".var/app/*/.pki/nssdb",            // Flatpak browsers
	".mozilla/firefox/*",               // Firefox profiles
	"snap/firefox/common/.mozilla/firefox/*",
	".var/app/org.mozilla.firefox/.mozilla/firefox/*",
}

// nssNicknamePrefix starts the nickname of every certificate the agent adds,
// followed by the reflector name and a fingerprint prefix.
const nssNicknamePrefix = "weft-local-dns-reflector"

// certutilTimeout bounds one certutil run (a locked database can stall it).
const certutilTimeout = 20 * time.Second

// nssCert is one CA certificate as it is added to an NSS database.
type nssCert struct {
	nickname string
	pem      []byte
}

// nssState is what a database held after the agent last touched it, so
// unchanged databases are not re-read with certutil every pass.
type nssState struct {
	modTime time.Time
	want    string
}

// certutilFunc runs certutil with args as uid/gid, feeding it stdin, and
// returns its combined output.
type certutilFunc func(ctx context.Context, uid, gid uint32, stdin []byte, args ...string) ([]byte, error)

// nssCerts splits a PEM bundle into the certificates to add, each with a
// nickname unique to this reflector and certificate.
func nssCerts(reflector string, caPEM []byte) []nssCert {
	var certs []nssCert
	for rest := caPEM; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			continue
		}
		sum := sha256.Sum256(block.Bytes)
		certs = append(certs, nssCert{
			nickname: fmt.Sprintf("%s %s %s", nssNicknamePrefix, reflector, hex.EncodeToString(sum[:6])),
			pem:      pem.EncodeToMemory(block),
		})
	}
	return certs
}

// findNSSDBs returns every NSS SQLite database directory under the home
// directories matched by homeGlobs (e.g. "/host/home/*", "/host/root").
func findNSSDBs(homeGlobs []string) []string {
	seen := map[string]bool{}
	var dbs []string
	for _, g := range homeGlobs {
		homes, _ := filepath.Glob(g)
		for _, home := range homes {
			for _, p := range nssDBPatterns {
				dirs, _ := filepath.Glob(filepath.Join(home, p))
				for _, d := range dirs {
					if seen[d] {
						continue
					}
					if fi, err := os.Stat(filepath.Join(d, "cert9.db")); err == nil && fi.Mode().IsRegular() {
						seen[d] = true
						dbs = append(dbs, d)
					}
				}
			}
		}
	}
	sort.Strings(dbs)
	return dbs
}

// listNSSNicknames returns the nicknames this reflector added to db, parsed
// from `certutil -L` (one "<nickname>   <trust>" row per certificate).
func listNSSNicknames(out []byte, reflector string) []string {
	prefix := nssNicknamePrefix + " " + reflector + " "
	var names []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(f) > 0 {
			names = append(names, prefix+f[0])
		}
	}
	return names
}

// applyNSSTrust makes every NSS database on the node trust exactly this
// reflector's current CAs (none, with nil caPEM). Browsers already running
// may need a restart to see a change. Failures are logged, once per database
// state, and never affect the hosts block: the proxy's real clients are
// containerd and pods, not browsers.
func (a *Agent) applyNSSTrust(ctx context.Context, caPEM []byte) {
	if len(a.cfg.NSSHomes) == 0 {
		return
	}
	if a.certutil == nil {
		path, err := exec.LookPath("certutil")
		if err != nil {
			if !a.certutilMissing {
				a.log.Info("certutil not found; browser trust stores are not updated", "reason", err.Error())
				a.certutilMissing = true
			}
			return
		}
		a.certutil = runCertutil(path)
	}
	want := nssCerts(a.cfg.Reflector, caPEM)
	wantKey := ""
	for _, c := range want {
		wantKey += c.nickname + ","
	}

	dbs := findNSSDBs(a.cfg.NSSHomes)
	present := map[string]bool{}
	for _, db := range dbs {
		present[db] = true
		fi, err := os.Stat(filepath.Join(db, "cert9.db"))
		if err != nil {
			continue
		}
		if s, ok := a.nss[db]; ok && s.want == wantKey && s.modTime.Equal(fi.ModTime()) {
			continue
		}
		if err := a.syncNSSDB(ctx, db, fi, want); err != nil {
			a.log.Error(err, "Cannot update browser trust store", "nssdb", db)
		}
		// Re-stat: certutil's own write changes the mtime.
		if fi, err = os.Stat(filepath.Join(db, "cert9.db")); err == nil {
			a.nss[db] = nssState{modTime: fi.ModTime(), want: wantKey}
		}
	}
	for db := range a.nss {
		if !present[db] {
			delete(a.nss, db)
		}
	}
}

// syncNSSDB adds the wanted certificates to one database and deletes this
// reflector's others, running certutil as the database's owner.
func (a *Agent) syncNSSDB(ctx context.Context, db string, fi os.FileInfo, want []nssCert) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("no owner for %s", db)
	}
	uid, gid := st.Uid, st.Gid
	dbArg := "sql:" + db
	out, err := a.certutil(ctx, uid, gid, nil, "-L", "-d", dbArg)
	if err != nil {
		return fmt.Errorf("list: %w: %s", err, bytes.TrimSpace(out))
	}
	have := map[string]bool{}
	for _, n := range listNSSNicknames(out, a.cfg.Reflector) {
		have[n] = true
	}
	var added, removed []string
	for _, c := range want {
		if have[c.nickname] {
			delete(have, c.nickname)
			continue
		}
		// -f /dev/null: a password-protected database fails instead of
		// prompting. "C,," trusts the CA to issue TLS server certificates.
		if out, err := a.certutil(ctx, uid, gid, c.pem, "-A", "-d", dbArg, "-n", c.nickname, "-t", "C,,", "-a", "-f", "/dev/null"); err != nil {
			return fmt.Errorf("add %s: %w: %s", c.nickname, err, bytes.TrimSpace(out))
		}
		added = append(added, c.nickname)
	}
	for n := range have {
		if out, err := a.certutil(ctx, uid, gid, nil, "-D", "-d", dbArg, "-n", n); err != nil {
			return fmt.Errorf("delete %s: %w: %s", n, err, bytes.TrimSpace(out))
		}
		removed = append(removed, n)
	}
	if len(added)+len(removed) > 0 {
		sort.Strings(removed)
		a.log.Info("Browser trust store updated", "nssdb", db, "added", added, "removed", removed)
	}
	return nil
}

// runCertutil runs the certutil at path as uid/gid. NSS_SDB_USE_CACHE=no
// skips NSS's probe that creates and deletes scratch files in the database
// directory to time the filesystem.
func runCertutil(path string) certutilFunc {
	return func(ctx context.Context, uid, gid uint32, stdin []byte, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, certutilTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = []string{"NSS_SDB_USE_CACHE=no", "PATH=/usr/bin:/bin"}
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}
		return cmd.CombinedOutput()
	}
}
