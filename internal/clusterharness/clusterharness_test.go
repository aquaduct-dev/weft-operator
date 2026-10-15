package clusterharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReport_JSONShape locks in the wire shape eval.sh's run_scenario
// jq query reads — {scenario, pass_conditions:[{id,label,passed,error}]}.
// Identical to openfactoryd/test/harness/firetest.go's reportPC; if
// either side drifts, this test trips.
func TestReport_JSONShape(t *testing.T) {
	r := &Report{
		Scenario: "scenario-1",
		PassConditions: []ReportPC{
			{ID: "1.1.foo", Label: "foo", Passed: true},
			{ID: "1.1.bar", Label: "bar", Passed: false, Error: "expected fizz"},
		},
	}

	tmp := filepath.Join(t.TempDir(), "report.json")
	if err := WriteReportJSON(tmp, r); err != nil {
		t.Fatalf("WriteReportJSON: %v", err)
	}

	buf, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(buf)

	// All field names eval.sh's jq query reads.
	for _, key := range []string{`"scenario"`, `"pass_conditions"`, `"id"`, `"label"`, `"passed"`, `"error"`} {
		if !strings.Contains(body, key) {
			t.Errorf("report JSON missing key %s: %s", key, body)
		}
	}

	// Round-trip preserves structure.
	var got Report
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Scenario != r.Scenario || len(got.PassConditions) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// TestAcquireNamespace_FailsWithoutEnv guards against silent fallbacks
// — if KATE_TEST_CLUSTER is empty we want a loud error, not the default
// kube context (which could be a personal cluster).
func TestAcquireNamespace_FailsWithoutEnv(t *testing.T) {
	t.Setenv("KATE_TEST_CLUSTER", "")
	t.Setenv("KATE_TEST_NAMESPACE", "")
	if _, err := AcquireNamespace(t.Context()); err == nil {
		t.Fatal("expected error when KATE_TEST_CLUSTER unset")
	}
}

func TestAcquireNamespace_Ok(t *testing.T) {
	t.Setenv("KATE_TEST_CLUSTER", "lab0")
	t.Setenv("KATE_TEST_NAMESPACE", "kate-test-thirdeye-0")
	lab, err := AcquireNamespace(t.Context())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if lab.Cluster != "lab0" || lab.Namespace != "kate-test-thirdeye-0" {
		t.Fatalf("lab=%+v", lab)
	}
}
