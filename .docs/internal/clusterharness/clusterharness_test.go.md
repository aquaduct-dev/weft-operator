<!-- docdaemon ts:2026-10-29T20:36:03Z hash:981e65bc57b41ec5 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the cluster harness, verifying JSON report serialization and environment-based namespace acquisition.

### TestReport_JSONShape
Ensures `WriteReportJSON` produces a wire format compatible with `eval.sh`'s `jq` queries. It validates that all expected keys (`scenario`, `pass_conditions`, etc.) are present in the output and confirms round-trip integrity via `json.Unmarshal`. This prevents drift between Go-side reporting and shell-side parsing.

### AcquireNamespace Tests
*   **TestAcquireNamespace_FailsWithoutEnv**: Verifies that `AcquireNamespace` returns an error when `KATE_TEST_CLUSTER` is unset, preventing silent fallbacks to local kube contexts.
*   **TestAcquireNamespace_Ok**: Confirms successful acquisition of cluster and namespace details from environment variables.