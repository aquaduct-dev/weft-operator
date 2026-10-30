<!-- docdaemon ts:2026-10-30T02:59:33Z hash:d2cdd86ba39e255c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the end-to-end test scenario for installing, upgrading, and uninstalling the Weft Operator Helm chart.

The `chart` struct manages state across steps, tracking legacy resource UIDs and VPA deployment identifiers to verify persistence during upgrades. `chartSteps` returns a sequence of assertions covering:

*   **Ownership Enforcement**: Verifies that Helm rejects CRDs lacking ownership metadata (`unownedCRDsRejected`) and handles conflicts when adopting CRDs previously managed by `kubectl-client-side-apply` via `--force-conflicts`.
*   **VPA Lifecycle**: Tests automatic installation of the Vertical Pod Autoscaler stack when `installController=auto`, ensuring deployments are created and the operator's own VPA object attaches correctly. It also validates that `installController=false` prevents any VPA resources from being installed.
*   **CRD Upgrades**: Confirms that existing Custom Resource instances survive schema changes (e.g., new defaults) and that the operator pod receives environment variables defined in `extraEnv`.
*   **Uninstall & Recovery**: Asserts that `helm uninstall` removes all release-managed resources except for the `WeftLocalDNSReflector` CRD, which gets stuck due to a finalizer conflict with CoreDNS. The step includes manual recovery logic to remove the finalizer and allow deletion.