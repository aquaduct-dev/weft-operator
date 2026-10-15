// Command k3s_smoke is the guest-side test binary for the
// firecracker_k3s_test smoke target. It runs INSIDE the microVM after
// k3s has been started by /sbin/init and asserts that:
//
//  1. The kubeconfig at /etc/rancher/k3s/k3s.yaml is readable.
//  2. `k3s kubectl get nodes -o json` reports at least one node with
//     status.conditions[Ready]=True.
//
// On pass it writes a lib/clusterharness.Report with a single PC
// `boot.k3s-ready` passed=true. On failure it writes a PC with the
// failure reason as `error`.
//
// The binary is statically linked (cgo-free Go) so it can live on the
// busybox-only rootfs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type reportPC struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

type report struct {
	Scenario       string     `json:"scenario"`
	PassConditions []reportPC `json:"pass_conditions"`
}

func main() {
	var scenarioPath, reportPath, kubeconfig, namespace string
	flag.StringVar(&scenarioPath, "scenario", "", "scenario YAML path (unused for smoke)")
	flag.StringVar(&reportPath, "report-json", "/tmp/report.json", "report JSON output path")
	flag.StringVar(&kubeconfig, "kubeconfig", "/etc/rancher/k3s/k3s.yaml", "kubeconfig path")
	flag.StringVar(&namespace, "namespace", "default", "namespace (unused)")
	flag.Parse()
	_ = scenarioPath
	_ = namespace

	rep := report{Scenario: "k3s_smoke"}

	if _, err := os.Stat(kubeconfig); err != nil {
		rep.PassConditions = append(rep.PassConditions, reportPC{
			ID: "boot.kubeconfig", Label: "kubeconfig-present", Passed: false,
			Error: fmt.Sprintf("kubeconfig stat: %v", err),
		})
		writeReport(reportPath, rep)
		os.Exit(1)
	}
	rep.PassConditions = append(rep.PassConditions, reportPC{
		ID: "boot.kubeconfig", Label: "kubeconfig-present", Passed: true,
	})

	if err := waitNodeReady(kubeconfig, 60*time.Second); err != nil {
		rep.PassConditions = append(rep.PassConditions, reportPC{
			ID: "boot.k3s-ready", Label: "k3s-ready", Passed: false,
			Error: err.Error(),
		})
		writeReport(reportPath, rep)
		os.Exit(1)
	}
	rep.PassConditions = append(rep.PassConditions, reportPC{
		ID: "boot.k3s-ready", Label: "k3s-ready", Passed: true,
	})

	writeReport(reportPath, rep)
}

func waitNodeReady(kubeconfig string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		cmd := exec.Command("/usr/local/bin/k3s", "kubectl",
			"--kubeconfig="+kubeconfig,
			"get", "nodes",
			"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.conditions[?(@.type=='Ready')].status}{\"\\n\"}{end}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("kubectl get nodes: %v: %s", err, string(out))
			time.Sleep(2 * time.Second)
			continue
		}
		if hasReady(string(out)) {
			return nil
		}
		lastErr = fmt.Errorf("no Ready nodes: %q", string(out))
		time.Sleep(2 * time.Second)
	}
	return lastErr
}

func hasReady(out string) bool {
	for _, line := range splitLines(out) {
		// Format: "<name>\tTrue" or "<name>\tFalse"
		for i := 0; i < len(line); i++ {
			if line[i] == '\t' {
				if line[i+1:] == "True" {
					return true
				}
				break
			}
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func writeReport(path string, r report) {
	buf, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		return
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
	}
}
