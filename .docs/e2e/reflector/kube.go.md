<!-- docdaemon ts:2026-10-29T20:35:56Z hash:cfdb574fa0726aed model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides Kubernetes helper utilities for end-to-end testing, focusing on pod lifecycle management and log retrieval.

### Purpose
The `scenario` type methods abstract common e2e operations: applying manifests via the host's `kubectl`, running one-shot client pods, and polling for state changes with progress logging.

### Key Functions
- **`waitFor`**: Generic polling loop that executes a `check` function at intervals. It logs progress every 15 seconds and returns detailed timeout errors including the last observed state.
- **`kubectlApply`**: Applies a manifest file using the VM’s local `kubectl` binary with server-side apply to handle large CRDs safely. It uses the scenario’s kubeconfig.
- **`runClientPod`**: Manages the full lifecycle of a test client pod:
  1. Ensures any existing pod with the same name is deleted.
  2. Creates a new pod using the predefined `clientImage`.
  3. Waits for the pod to reach a terminal state (`Succeeded` or `Failed`).
  4. Retrieves and returns the container logs.
  5. Fails if the exit code is non-zero.
- **`podLogs`**: Fetches the complete log output for a specific container in a namespace.
- **`containerState`**: Helper to summarize why a container is not running (Waiting, Terminated, or Running).

### Design Notes
- The `clientImage` constant points to a specific e2e client image.
- Pods are created in the `e2e-client` namespace.
- The `ptr` helper simplifies pointer creation for struct fields like `AutomountServiceAccountToken`.