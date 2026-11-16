// nssdb_test.go — browser NSS trust store sync: database discovery under
// home directories, certutil add/delete against a fake certutil that keeps
// nicknames per database, the unchanged-database skip, and cleanup.
package nodeagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// fakeCertutil models certutil -L/-A/-D over per-database nickname sets.
type fakeCertutil struct {
	dbs   map[string][]string // "sql:<dir>" -> nicknames
	calls []string
	uids  []uint32
}

func (f *fakeCertutil) run(_ context.Context, uid, _ uint32, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args[0])
	f.uids = append(f.uids, uid)
	db := args[slices.Index(args, "-d")+1]
	switch args[0] {
	case "-L":
		out := "\nCertificate Nickname                                         Trust Attributes\n                                                             SSL,S/MIME,JAR/XPI\n\n"
		for _, n := range f.dbs[db] {
			out += fmt.Sprintf("%-60s C,,\n", n)
		}
		return []byte(out), nil
	case "-A":
		if !strings.Contains(string(stdin), "BEGIN CERTIFICATE") {
			return []byte("no cert on stdin"), fmt.Errorf("exit 255")
		}
		f.dbs[db] = append(f.dbs[db], args[slices.Index(args, "-n")+1])
		touch(strings.TrimPrefix(db, "sql:"))
	case "-D":
		n := args[slices.Index(args, "-n")+1]
		f.dbs[db] = slices.DeleteFunc(f.dbs[db], func(s string) bool { return s == n })
		touch(strings.TrimPrefix(db, "sql:"))
	}
	return nil, nil
}

func touch(dir string) {
	p := filepath.Join(dir, "cert9.db")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, 'x'), 0o600)
}

func mkNSSDB(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "cert9.db"), nil, 0o600)
}

func TestFindNSSDBs(t *testing.T) {
	root := t.TempDir()
	mkNSSDB(t, filepath.Join(root, "home/alice/.pki/nssdb"))
	mkNSSDB(t, filepath.Join(root, "home/alice/.mozilla/firefox/abc.default-release"))
	mkNSSDB(t, filepath.Join(root, "home/bob/snap/firefox/common/.mozilla/firefox/x.default"))
	mkNSSDB(t, filepath.Join(root, "root/.pki/nssdb"))
	// Legacy DBM database and an empty Firefox dir: skipped.
	os.MkdirAll(filepath.Join(root, "home/carol/.pki/nssdb"), 0o700)
	os.WriteFile(filepath.Join(root, "home/carol/.pki/nssdb/cert8.db"), nil, 0o600)
	os.MkdirAll(filepath.Join(root, "home/alice/.mozilla/firefox/Crash Reports"), 0o700)

	got := findNSSDBs([]string{filepath.Join(root, "home/*"), filepath.Join(root, "root"), filepath.Join(root, "missing")})
	for i := range got {
		got[i] = strings.TrimPrefix(got[i], root+"/")
	}
	want := []string{
		"home/alice/.mozilla/firefox/abc.default-release",
		"home/alice/.pki/nssdb",
		"home/bob/snap/firefox/common/.mozilla/firefox/x.default",
		"root/.pki/nssdb",
	}
	t.Logf("found %v", got)
	if !slices.Equal(got, want) {
		t.Errorf("findNSSDBs = %v, want %v", got, want)
	}
}

func TestNSSCerts(t *testing.T) {
	a, b := testCAPEM(t), testCAPEM(t)
	junk := []byte("-----BEGIN PRIVATE KEY-----\nZm9v\n-----END PRIVATE KEY-----\n")
	certs := nssCerts("lab0", append(append(append([]byte{}, a...), junk...), b...))
	if len(certs) != 2 {
		t.Fatalf("got %d certs, want 2", len(certs))
	}
	for _, c := range certs {
		t.Logf("nickname %q", c.nickname)
		if !strings.HasPrefix(c.nickname, "weft-local-dns-reflector lab0 ") || len(strings.Fields(c.nickname)) != 3 {
			t.Errorf("bad nickname %q", c.nickname)
		}
	}
	if certs[0].nickname == certs[1].nickname {
		t.Errorf("distinct CAs share a nickname")
	}
	if again := nssCerts("lab0", a); again[0].nickname != certs[0].nickname {
		t.Errorf("nickname not stable")
	}
}

func TestApplyNSSTrust(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "home/alice/.pki/nssdb")
	mkNSSDB(t, db)
	fake := &fakeCertutil{dbs: map[string][]string{
		// A user's own CA and another reflector's are left alone.
		"sql:" + db: {"My Corp Root", "weft-local-dns-reflector other 0123456789ab"},
	}}
	a := NewAgent(Config{Reflector: "lab0", NSSHomes: []string{filepath.Join(root, "home/*")}}, zap.New())
	a.certutil = fake.run
	ctx := context.Background()

	caA := testCAPEM(t)
	a.applyNSSTrust(ctx, caA)
	nickA := nssCerts("lab0", caA)[0].nickname
	t.Logf("after add: %v (calls %v)", fake.dbs["sql:"+db], fake.calls)
	if !slices.Contains(fake.dbs["sql:"+db], nickA) {
		t.Fatalf("CA not added")
	}
	if uid := uint32(os.Getuid()); fake.uids[0] != uid {
		t.Errorf("certutil ran as uid %d, want the database owner %d", fake.uids[0], uid)
	}

	// Unchanged database and CA: certutil is not run again.
	n := len(fake.calls)
	a.applyNSSTrust(ctx, caA)
	if len(fake.calls) != n {
		t.Errorf("unchanged database re-synced: %v", fake.calls[n:])
	}

	// CA rotation: the new one is added, the old one removed.
	caB := testCAPEM(t)
	a.applyNSSTrust(ctx, caB)
	nickB := nssCerts("lab0", caB)[0].nickname
	got := slices.Clone(fake.dbs["sql:"+db])
	sort.Strings(got)
	t.Logf("after rotation: %v", got)
	want := []string{"My Corp Root", nickB, "weft-local-dns-reflector other 0123456789ab"}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("after rotation = %v, want %v", got, want)
	}

	// A database created later (browser first launched) is picked up.
	db2 := filepath.Join(root, "home/bob/.mozilla/firefox/p.default")
	mkNSSDB(t, db2)
	a.applyNSSTrust(ctx, caB)
	if !slices.Contains(fake.dbs["sql:"+db2], nickB) {
		t.Errorf("new database not synced: %v", fake.dbs["sql:"+db2])
	}

	// Cleanup removes only this reflector's certificates.
	a.applyNSSTrust(ctx, nil)
	t.Logf("after cleanup: %v / %v", fake.dbs["sql:"+db], fake.dbs["sql:"+db2])
	if slices.Contains(fake.dbs["sql:"+db], nickB) || len(fake.dbs["sql:"+db2]) != 0 {
		t.Errorf("cleanup left reflector CAs")
	}
	if len(fake.dbs["sql:"+db]) != 2 {
		t.Errorf("cleanup touched foreign entries: %v", fake.dbs["sql:"+db])
	}
}

func TestApplyNSSTrustNoCertutil(t *testing.T) {
	root := t.TempDir()
	mkNSSDB(t, filepath.Join(root, "home/alice/.pki/nssdb"))
	t.Setenv("PATH", t.TempDir())
	a := NewAgent(Config{Reflector: "lab0", NSSHomes: []string{filepath.Join(root, "home/*")}}, zap.New())
	a.applyNSSTrust(context.Background(), testCAPEM(t))
	if !a.certutilMissing || a.certutil != nil {
		t.Errorf("missing certutil not detected")
	}
}
