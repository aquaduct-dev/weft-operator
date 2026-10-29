<!-- docdaemon ts:2026-10-29T20:36:05Z hash:fba35a4349d611da model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This package defines the JSON contract and runtime helpers for reporting test scenario outcomes from guest-side firecracker_k3s_test binaries to the host launcher.

### Purpose
It bridges the gap between isolated microVM tests and the evaluation script (`eval.sh`). The `Report` struct mirrors the exact JSON schema expected by `run_scenario`, ensuring stable parsing via `jq`.

### Key Types & Functions
*   **`Report` / `ReportPC`**: Structs defining the pass/fail status of individual conditions within a named scenario.
*   **`Runner` Interface**: Allows scenarios to inject logic that returns `[]ReportPC` results given a context and a `Lab`. `RunnerFunc` provides an adapter for simple functions.
*   **`WriteReportJSON`**: Marshals the report to disk with consistent indentation, matching the output format of `firetest.go` so both substrates are readable by the same query tools.
*   **`AcquireNamespace`**: Resolves the target environment into a `Lab` struct. It prioritizes Firecracker hermetic mode (checking `FC_KUBECONFIG`/`KUBECONFIG`) over live cluster mode (`KATE_TEST_CLUSTER`/`KATE_TEST_NAMESPACE`). This dual-substrate support allows the same harness code to run in airgapped VMs or against existing clusters.

### Context
The namespace resolution logic supports the e2e reflector test, which boots k3s in an airgapped microVM. By detecting the presence of a local kubeconfig, the harness avoids requiring a live lab0 dependency, enabling self-contained validation of in-cluster DNS reflection.