// Package clusterharness is the report-JSON contract between a guest-side
// firecracker_k3s_test binary and the host launcher (tools/firecracker).
// Imported from openfactory's lib/clusterharness (commit 194e70a9).
//
// The contract (matched literally by eval.sh's run_scenario function):
//
//	{
//	  "scenario": "<name>",
//	  "pass_conditions": [
//	    {"id": "<X.Y.Z>", "label": "<short>", "passed": <bool>, "error": "<msg>"}
//	  ]
//	}
//
// The Runner interface lets each scenario contribute one or more
// ReportPC entries. AcquireNamespace resolves the per-test ephemeral
// namespace from KATE_TEST_CLUSTER / KATE_TEST_NAMESPACE — the same
// envvar contract kate_integration_test relies on.
package clusterharness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// ReportPC mirrors openfactoryd/test/harness/firetest.go's reportPC; the
// JSON field names are part of eval.sh's run_scenario contract.
type ReportPC struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

// Report is the canonical scenario-result payload.
type Report struct {
	Scenario       string     `json:"scenario"`
	PassConditions []ReportPC `json:"pass_conditions"`
}

// Runner is implemented by every scenario module — given a context (and
// a Lab acquired via AcquireNamespace), it returns the list of PC
// outcomes to record in the report.
type Runner interface {
	Run(ctx context.Context, lab *Lab) []ReportPC
}

// RunnerFunc adapts a plain function to the Runner interface.
type RunnerFunc func(ctx context.Context, lab *Lab) []ReportPC

// Run satisfies Runner for RunnerFunc.
func (f RunnerFunc) Run(ctx context.Context, lab *Lab) []ReportPC { return f(ctx, lab) }

// WriteReportJSON marshals a Report to the given path with stable
// indentation. Mirrors firetest's writer so eval.sh's jq query reads
// both the same way.
func WriteReportJSON(path string, r *Report) error {
	buf, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}

// Lab represents the kate_integration_test ephemeral namespace the
// scenario gets to operate inside. The cluster field maps to the lab0
// kube context; the namespace is unique per scenario run to keep
// concurrent harness runs collision-free.
type Lab struct {
	Cluster   string
	Namespace string
}

// AcquireNamespace resolves the per-test Lab. Two substrates are
// supported, in priority order:
//
//  1. **Firecracker hermetic cluster.** If KUBECONFIG (or FC_KUBECONFIG)
//     points to a readable file, the caller is running inside a
//     firecracker_k3s_test microVM — return Lab{Cluster:"firecracker",
//     Namespace: $KATE_TEST_NAMESPACE or "default"}. No live lab0
//     dependency.
//
//  2. **Live lab0 cluster.** KATE_TEST_CLUSTER + KATE_TEST_NAMESPACE
//     envvars (the legacy kate_integration_test substrate). Empty
//     cluster → returns an error so the caller can crit_fail loudly
//     rather than silently hit the default kube context.
func AcquireNamespace(_ context.Context) (*Lab, error) {
	if kc := firstSetEnv("FC_KUBECONFIG", "KUBECONFIG"); kc != "" {
		if _, err := os.Stat(kc); err == nil {
			ns := os.Getenv("KATE_TEST_NAMESPACE")
			if ns == "" {
				ns = "default"
			}
			return &Lab{Cluster: "firecracker", Namespace: ns}, nil
		}
	}
	cluster := os.Getenv("KATE_TEST_CLUSTER")
	if cluster == "" {
		return nil, fmt.Errorf("KATE_TEST_CLUSTER is unset (and no FC_KUBECONFIG/KUBECONFIG file present)")
	}
	ns := os.Getenv("KATE_TEST_NAMESPACE")
	if ns == "" {
		return nil, fmt.Errorf("KATE_TEST_NAMESPACE is unset")
	}
	return &Lab{Cluster: cluster, Namespace: ns}, nil
}

func firstSetEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
