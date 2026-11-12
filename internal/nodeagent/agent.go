// agent.go — the `node-agent` subcommand of the operator binary, run by a
// WeftLocalDNSReflector's DaemonSet on every node. Kubelet/containerd image
// pulls resolve through the host's resolver, not CoreDNS, so the agent makes
// the node itself use the in-cluster proxy:
//
//   - the reflector CA appended (in a managed block) to the host's distro CA
//     bundles, so host processes trust the proxy (always present)
//   - containerd certs.d entries that trust the reflector CA (always present,
//     except where someone else, e.g. k3s from registries.yaml, owns the
//     hostname's hosts.toml)
//   - an /etc/hosts block mapping the hostnames to the proxy Service IP,
//     present ONLY while a TLS handshake to that IP verifies for every
//     hostname. If the proxy goes away the block is withdrawn and pulls fall
//     back to the public path instead of failing. A hostname is in the block
//     only while containerd on this node trusts the proxy for it (see
//     containerdTrusted); pointing containerd at a certificate it cannot
//     verify breaks every pull from that host.
//
// On SIGTERM (DaemonSet deleted or rolled) the agent removes everything it
// wrote before exiting.
package nodeagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Config is the agent's command line.
type Config struct {
	Reflector string
	ServiceIP string
	Hostnames []string
	CAFile    string
	HostsFile string
	// HostTrustFiles are distro CA bundles to add the CA to; missing
	// files are skipped (a node has one or the other).
	HostTrustFiles []string
	CertsDir       string
	// HostProc is the host's /proc, mounted read-only, used to find when
	// containerd started. Empty disables that check, so only hostnames
	// with an agent-owned certs.d entry are pointed at the proxy.
	HostProc string
	Interval time.Duration
}

// Run parses args, runs the agent until SIGINT/SIGTERM, cleans up, and
// returns the process exit code.
func Run(args []string) int {
	logger := zap.New()
	cfg, err := parseFlags(args)
	if err != nil {
		logger.Error(err, "Invalid node-agent flags")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := NewAgent(cfg, logger).Run(ctx); err != nil {
		logger.Error(err, "node-agent exited with error")
		return 1
	}
	return 0
}

func parseFlags(args []string) (Config, error) {
	fs := flag.NewFlagSet("node-agent", flag.ContinueOnError)
	var cfg Config
	var hostnames, trustFiles string
	fs.StringVar(&cfg.Reflector, "reflector", "", "WeftLocalDNSReflector name (tags everything the agent writes)")
	fs.StringVar(&cfg.ServiceIP, "service-ip", "", "ClusterIP of the reflector proxy Service")
	fs.StringVar(&hostnames, "hostnames", "", "Comma-separated hostnames to point at the proxy")
	fs.StringVar(&cfg.CAFile, "ca-file", "", "PEM CA bundle the proxy certificate chains to")
	fs.StringVar(&cfg.HostsFile, "hosts-file", "/host/etc/hosts", "The node's /etc/hosts, mounted into the pod")
	fs.StringVar(&trustFiles, "host-trust-files", "", "Comma-separated host CA bundle files (as mounted) to add the CA to")
	fs.StringVar(&cfg.CertsDir, "certs-dir", "", "containerd registry config_path (certs.d), mounted at the same path")
	fs.StringVar(&cfg.HostProc, "host-proc", "", "The host's /proc, mounted read-only, to read containerd's start time")
	fs.DurationVar(&cfg.Interval, "interval", 15*time.Second, "How often to health-check the proxy and re-assert node state")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	for h := range strings.SplitSeq(hostnames, ",") {
		if h = strings.TrimSpace(strings.ToLower(h)); h != "" {
			cfg.Hostnames = append(cfg.Hostnames, h)
		}
	}
	sort.Strings(cfg.Hostnames)
	for f := range strings.SplitSeq(trustFiles, ",") {
		if f = strings.TrimSpace(f); f != "" {
			cfg.HostTrustFiles = append(cfg.HostTrustFiles, f)
		}
	}
	switch {
	case cfg.Reflector == "":
		return cfg, errors.New("--reflector is required")
	case net.ParseIP(cfg.ServiceIP) == nil:
		return cfg, fmt.Errorf("--service-ip %q is not an IP", cfg.ServiceIP)
	case len(cfg.Hostnames) == 0:
		return cfg, errors.New("--hostnames is required")
	case cfg.CAFile == "" || cfg.CertsDir == "":
		return cfg, errors.New("--ca-file and --certs-dir are required")
	}
	return cfg, nil
}

// Agent holds the loop state.
type Agent struct {
	cfg    Config
	log    logr.Logger
	dialer func(ctx context.Context, addr, serverName string, roots *x509.CertPool) error
	// containerdStart reports when the node's containerd started, and the
	// process name it went by.
	containerdStart func() (time.Time, string, error)
	now             func() time.Time
	// healthy is the last published state, so only transitions are logged.
	healthy *bool
	// untrusted is the last logged set of hostnames held off the proxy
	// because containerd would not trust it (nil before the first pass).
	untrusted *string
}

// NewAgent builds an agent with the real TLS dialer and host /proc reader.
func NewAgent(cfg Config, logger logr.Logger) *Agent {
	return &Agent{
		cfg:             cfg,
		log:             logger.WithValues("reflector", cfg.Reflector),
		dialer:          tlsHandshake,
		containerdStart: func() (time.Time, string, error) { return containerdStart(cfg.HostProc) },
		now:             time.Now,
	}
}

// Run loops until ctx is done, then removes everything the agent wrote.
func (a *Agent) Run(ctx context.Context) error {
	a.log.Info("node-agent starting", "serviceIP", a.cfg.ServiceIP, "hostnames", a.cfg.Hostnames, "hostsFile", a.cfg.HostsFile, "certsDir", a.cfg.CertsDir)
	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()
	for {
		a.Tick(ctx)
		select {
		case <-ctx.Done():
			a.log.Info("Shutting down; removing node state")
			return a.Cleanup()
		case <-ticker.C:
		}
	}
}

// Tick runs one check-and-apply pass. Errors are logged, not returned: one
// bad pass must not stop later passes from repairing node state.
func (a *Agent) Tick(ctx context.Context) {
	caPEM, err := os.ReadFile(a.cfg.CAFile)
	if err != nil {
		a.log.Error(err, "Cannot read CA bundle; withdrawing hosts entry", "caFile", a.cfg.CAFile)
		a.applyHosts(nil)
		return
	}

	// containerd trust first, so by the time /etc/hosts points at the
	// proxy the CA is already in place. k3s rewrites certs.d on restart,
	// which is why this is re-asserted every pass rather than once.
	// certsD holds, per hostname, why its entry could not be written (nil
	// when the agent owns it). Failures are reported by containerdTrusted
	// once per change rather than every pass.
	certsD := map[string]error{}
	for _, h := range a.cfg.Hostnames {
		changed, err := writeCertsD(a.cfg.CertsDir, a.cfg.Reflector, h, caPEM)
		certsD[h] = err
		if err != nil {
			a.log.V(1).Info("containerd trust not written", "hostname", h, "reason", err.Error())
		} else if changed {
			a.log.Info("containerd trust written", "hostname", h)
		}
	}
	a.pruneCertsD()
	caSince, caOK := a.applyHostTrust(caPEM)

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		a.log.Error(errors.New("no certificates in CA bundle"), "Withdrawing hosts entry", "caFile", a.cfg.CAFile)
		a.applyHosts(nil)
		return
	}
	err = a.checkProxy(ctx, roots)
	healthy := err == nil
	if a.healthy == nil || *a.healthy != healthy {
		if healthy {
			a.log.Info("Proxy verified; pointing node at it", "serviceIP", a.cfg.ServiceIP)
		} else {
			a.log.Info("Proxy not verifiable; node uses the public path", "reason", err.Error())
		}
		a.healthy = &healthy
	}
	if healthy {
		a.applyHosts(a.containerdTrusted(certsD, caSince, caOK))
	} else {
		a.applyHosts(nil)
	}
}

// startMargin absorbs the coarseness of the two times compared in
// containerdTrusted: the bundle stamp is whole seconds taken just before the
// write, and /proc's btime is whole seconds (and moves slightly with clock
// adjustments). A containerd that started within it is treated as having
// started before the CA landed.
const startMargin = 5 * time.Second

// containerdTrusted returns the hostnames containerd on this node will
// accept the proxy's certificate for, logging when that set changes:
//
//   - those whose certs.d entry the agent owns (certsD), since containerd
//     reads certs.d on every pull; or
//   - all of them, when the reflector CA is in the host CA bundles (since
//     caSince) and containerd started after that. Go loads system roots
//     once per process, so a containerd started earlier never sees it.
//
// A hostname whose hosts.toml someone else owns (k3s writes one per
// registries.yaml mirror) gets only the second route; until containerd
// restarts it stays on the public path rather than failing every pull.
func (a *Agent) containerdTrusted(certsD map[string]error, caSince time.Time, caOK bool) []string {
	var trusted, untrusted []string
	var certsDErrs []string
	for _, h := range a.cfg.Hostnames {
		if err, tried := certsD[h]; tried && err == nil {
			trusted = append(trusted, h)
		} else {
			untrusted = append(untrusted, h)
			if err != nil {
				certsDErrs = append(certsDErrs, err.Error())
			}
		}
	}
	if len(untrusted) == 0 {
		a.logUntrusted(nil, "", nil)
		return trusted
	}

	var reason string
	if !caOK {
		reason = "reflector CA is not in every host CA bundle"
	} else if started, comm, err := a.containerdStart(); err != nil {
		reason = "cannot tell when containerd started: " + err.Error()
	} else if !started.After(caSince.Add(startMargin)) {
		reason = fmt.Sprintf("%s started %s, before the reflector CA reached the host CA bundle at %s; it trusts the proxy once restarted",
			comm, started.UTC().Format(time.RFC3339), caSince.UTC().Format(time.RFC3339))
	} else {
		a.logUntrusted(nil, "", nil)
		return a.cfg.Hostnames
	}
	a.logUntrusted(untrusted, reason, certsDErrs)
	return trusted
}

// logUntrusted logs the hostnames held on the public path for want of
// containerd trust, only when that set changes (the reason alone, e.g. a
// refreshed timestamp, is not a change).
func (a *Agent) logUntrusted(hostnames []string, reason string, certsDErrs []string) {
	key := strings.Join(hostnames, ",")
	if a.untrusted != nil && *a.untrusted == key {
		return
	}
	if len(hostnames) > 0 {
		a.log.Info("containerd would not trust the proxy; hostnames stay on the public path", "untrustedHostnames", hostnames, "reason", reason, "certsD", certsDErrs)
	} else if a.untrusted != nil {
		a.log.Info("containerd trusts the proxy for every hostname")
	}
	a.untrusted = &key
}

// checkProxy completes a verified TLS handshake for every hostname.
func (a *Agent) checkProxy(ctx context.Context, roots *x509.CertPool) error {
	addr := net.JoinHostPort(a.cfg.ServiceIP, "443")
	for _, h := range a.cfg.Hostnames {
		if err := a.dialer(ctx, addr, h, roots); err != nil {
			return fmt.Errorf("%s via %s: %w", h, addr, err)
		}
	}
	return nil
}

// applyHosts sets (or with nil, removes) the reflector's /etc/hosts block.
// The file is a single-file bind mount, so it is rewritten in place rather
// than renamed over (a rename would fail with EBUSY).
func (a *Agent) applyHosts(hostnames []string) {
	current, err := os.ReadFile(a.cfg.HostsFile)
	if err != nil {
		a.log.Error(err, "Cannot read hosts file", "hostsFile", a.cfg.HostsFile)
		return
	}
	next := withHostsBlock(string(current), a.cfg.Reflector, a.cfg.ServiceIP, hostnames)
	if next == string(current) {
		return
	}
	f, err := os.OpenFile(a.cfg.HostsFile, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		a.log.Error(err, "Cannot open hosts file for writing", "hostsFile", a.cfg.HostsFile)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(next); err != nil {
		a.log.Error(err, "Failed writing hosts file", "hostsFile", a.cfg.HostsFile)
		return
	}
	a.log.Info("Hosts file updated", "hostnames", hostnames, "serviceIP", a.cfg.ServiceIP)
}

// applyHostTrust sets (or with nil, removes) the CA block in every host CA
// bundle that exists. Programs already running keep the roots they loaded;
// new processes pick the CA up. Distro tooling (update-ca-certificates)
// regenerates the bundle and drops the block, which the next pass restores.
//
// It returns since, the latest time the current CAs landed in any of the
// bundles (from each block's stamp), with ok=true only when at least one
// bundle exists and every existing one holds the CAs after this pass.
func (a *Agent) applyHostTrust(caPEM []byte) (since time.Time, ok bool) {
	want := strings.TrimSpace(string(caPEM))
	ok = want != ""
	found := false
	for _, f := range a.cfg.HostTrustFiles {
		current, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			a.log.Error(err, "Cannot read host CA bundle", "file", f)
			ok = false
			continue
		}
		found = true
		info, err := os.Stat(f)
		if err != nil {
			a.log.Error(err, "Cannot stat host CA bundle", "file", f)
			ok = false
			continue
		}
		stamp := a.now().Truncate(time.Second)
		if have, haveSince, exists := readCABlock(string(current), a.cfg.Reflector); exists && have == want {
			switch {
			case !haveSince.IsZero() && !haveSince.After(stamp):
				stamp = haveSince
			case haveSince.IsZero() && info.ModTime().Before(stamp):
				// An older agent wrote the block without a stamp. The
				// file has held it unchanged since at least its mtime.
				stamp = info.ModTime()
			}
		}
		next := withCABlock(string(current), a.cfg.Reflector, caPEM, stamp)
		if next != string(current) {
			if _, err := writeIfChanged(f, []byte(next), info.Mode().Perm()); err != nil {
				a.log.Error(err, "Failed writing host CA bundle", "file", f)
				ok = false
				continue
			}
			a.log.Info("Host CA bundle updated", "file", f, "present", want != "")
		}
		if stamp.After(since) {
			since = stamp
		}
	}
	return since, ok && found
}

// pruneCertsD removes certs.d entries for hostnames no longer configured.
func (a *Agent) pruneCertsD() {
	want := map[string]bool{}
	for _, h := range a.cfg.Hostnames {
		want[h] = true
	}
	for _, h := range ownedHostnames(a.cfg.CertsDir, a.cfg.Reflector) {
		if want[h] {
			continue
		}
		if removed, err := removeCertsD(a.cfg.CertsDir, a.cfg.Reflector, h); err != nil {
			a.log.Error(err, "Failed removing stale containerd trust", "hostname", h)
		} else if removed {
			a.log.Info("Removed stale containerd trust", "hostname", h)
		}
	}
}

// Cleanup removes the hosts block and every certs.d entry this reflector owns.
func (a *Agent) Cleanup() error {
	a.applyHosts(nil)
	a.applyHostTrust(nil)
	var errs []error
	for _, h := range ownedHostnames(a.cfg.CertsDir, a.cfg.Reflector) {
		if _, err := removeCertsD(a.cfg.CertsDir, a.cfg.Reflector, h); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func tlsHandshake(ctx context.Context, addr, serverName string, roots *x509.CertPool) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{ServerName: serverName, RootCAs: roots, MinVersion: tls.VersionTLS12}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}
