<!-- docdaemon ts:2026-10-29T20:36:49Z hash:7e4288037a611a4d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This binary validates that a k3s cluster inside a microVM is bootstrapped and ready to accept workloads.

`k3s_smoke` runs as the guest-side test for `firecracker_k3s_test`. It verifies two critical conditions: the presence of the kubeconfig file and the availability of at least one node in a `Ready` state. The tool is statically linked to ensure compatibility with the minimal busybox rootfs used by the microVM.

### Execution Flow

1.  **Argument Parsing**: Accepts flags for report output path, kubeconfig location, and namespace (unused).
2.  **Kubeconfig Check**: Verifies `/etc/rancher/k3s/k3s.yaml` exists. If missing, it writes a failure report and exits.
3.  **Node Readiness Polling**: Calls `waitNodeReady`, which repeatedly executes `k3s kubectl get nodes` until a node reports `status.conditions[?(@.type=='Ready')].status=True` or the timeout expires.
4.  **Reporting**: Writes a JSON report to the specified path containing pass/fail status for `boot.kubeconfig` and `boot.k3s-ready`.

### Key Components

*   **`reportPC` / `report`**: Structs defining the schema for the lib/clusterharness report format.
*   **`waitNodeReady`**: Implements the polling loop with a fixed timeout and backoff interval.
*   **`hasReady`**: Parses the JSONPath output line-by-line to detect `True` status without requiring heavy JSON unmarshaling.