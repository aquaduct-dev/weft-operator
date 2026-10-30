// Command reflectore2e is the guest side of the WeftLocalDNSReflector e2e
// tests (//e2e/reflector:reflector_test, :default_reflector_test). It is one
// static binary with three modes:
//
//   - (default) scenario: runs on the firecracker microVM next to k3s,
//     drives a scenario through the API server and writes a clusterharness
//     report. The scenario name comes from --scenario: "reflector"
//     (scenario.go) or "default-reflector" (default_scenario.go);
//   - "client": runs inside a pod and does what a real workload would:
//     resolve the reflected hostname, make a TLS-verified HTTPS request using
//     nothing but the system trust store, and push an image (client.go);
//   - "sleep": the entrypoint of the image the client pushes, so a pod can
//     run it after containerd pulls it back through the reflector;
//   - "fetch": a plain-HTTP GET of a reflected http:// route (fetch.go).
//
// The "reflector-claims" and "reflector-lifecycle" scenarios
// (//e2e/reflector:reflector_claims_test, :reflector_lifecycle_test) run on
// //e2e/e2ekit instead; see extras.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
	"aquaduct.dev/weft-operator/internal/clusterharness"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "client":
			os.Exit(runClient(os.Args[2:]))
		case "sleep":
			log.Printf("sleep: pulled image is running")
			select {}
		case "fetch":
			os.Exit(runFetch(os.Args[2:]))
		}
	}
	if _, ok := extraScenarios[scenarioArg()]; ok {
		os.Exit(e2ekit.Main(extraScenarios))
	}
	os.Exit(runScenario())
}

func runScenario() int {
	var scenarioPath, reportPath, kubeconfig, namespace string
	flag.StringVar(&scenarioPath, "scenario", "", "scenario YAML (namespaces; applied by the VM init)")
	flag.StringVar(&reportPath, "report-json", "/tmp/report.json", "clusterharness report output")
	flag.StringVar(&kubeconfig, "kubeconfig", "/etc/rancher/k3s/k3s.yaml", "kubeconfig")
	flag.StringVar(&namespace, "namespace", "default", "unused; the scenario uses its own namespaces")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	// The VM init passes --scenario=/etc/manifests/<scenario>.yaml.
	name := strings.TrimSuffix(filepath.Base(scenarioPath), ".yaml")
	report := &clusterharness.Report{Scenario: name}
	s, err := newScenario(kubeconfig)
	switch {
	case err != nil:
		report.PassConditions = []clusterharness.ReportPC{{ID: "e2e.setup", Label: "kube-client", Error: err.Error()}}
	case name == "default-reflector":
		s.refl = defaultReflectorName
		report.PassConditions = s.run(ctx, s.defaultReflectorSteps())
	default:
		report.PassConditions = s.run(ctx, s.reflectorSteps())
	}
	if err := clusterharness.WriteReportJSON(reportPath, report); err != nil {
		log.Printf("write report: %v", err)
		return 1
	}
	for _, pc := range report.PassConditions {
		if !pc.Passed {
			fmt.Printf("FAILED %s (%s): %s\n", pc.ID, pc.Label, pc.Error)
			return 1
		}
	}
	return 0
}

// scenarioArg is the scenario name from --scenario, read before flags are
// parsed so the e2ekit scenarios can parse them their own way.
func scenarioArg() string {
	for i, a := range os.Args[1:] {
		for _, prefix := range []string{"--scenario=", "-scenario="} {
			if v, ok := strings.CutPrefix(a, prefix); ok {
				return strings.TrimSuffix(filepath.Base(v), ".yaml")
			}
		}
		if (a == "--scenario" || a == "-scenario") && i+2 < len(os.Args) {
			return strings.TrimSuffix(filepath.Base(os.Args[i+2]), ".yaml")
		}
	}
	return ""
}
