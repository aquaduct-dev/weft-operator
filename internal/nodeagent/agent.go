// agent.go — the `node-agent` subcommand of the operator binary, run by a
// WeftLocalDNSReflector's DaemonSet on every node. Kubelet/containerd image
// pulls resolve through the host's resolver, not CoreDNS, so the agent makes
// the node itself use the in-cluster proxy:
//
//   - the reflector CA appended (in a managed block) to the host's distro CA
//     bundles, so host processes trust the proxy (always present)
//   - containerd certs.d entries that trust the reflector CA (always present)
//   - an /etc/hosts block mapping the hostnames to the proxy Service IP,
//     present ONLY while a TLS handshake to that IP verifies for every
//     hostname. If the proxy goes away the block is withdrawn and pulls fall
//     back to the public path instead of failing.
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
	Interval       time.Duration
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
	// healthy is the last published state, so only transitions are logged.
	healthy *bool
}

// NewAgent builds an agent with the real TLS dialer.
func NewAgent(cfg Config, logger logr.Logger) *Agent {
	return &Agent{cfg: cfg, log: logger.WithValues("reflector", cfg.Reflector), dialer: tlsHandshake}
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
	for _, h := range a.cfg.Hostnames {
		changed, err := writeCertsD(a.cfg.CertsDir, a.cfg.Reflector, h, caPEM)
		if err != nil {
			a.log.Error(err, "containerd trust not written", "hostname", h)
		} else if changed {
			a.log.Info("containerd trust written", "hostname", h)
		}
	}
	a.pruneCertsD()
	a.applyHostTrust(caPEM)

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
		a.applyHosts(a.cfg.Hostnames)
	} else {
		a.applyHosts(nil)
	}
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
func (a *Agent) applyHostTrust(caPEM []byte) {
	for _, f := range a.cfg.HostTrustFiles {
		current, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			a.log.Error(err, "Cannot read host CA bundle", "file", f)
			continue
		}
		next := withCABlock(string(current), a.cfg.Reflector, caPEM)
		if next == string(current) {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			a.log.Error(err, "Cannot stat host CA bundle", "file", f)
			continue
		}
		if _, err := writeIfChanged(f, []byte(next), info.Mode().Perm()); err != nil {
			a.log.Error(err, "Failed writing host CA bundle", "file", f)
			continue
		}
		a.log.Info("Host CA bundle updated", "file", f, "present", len(caPEM) > 0)
	}
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
