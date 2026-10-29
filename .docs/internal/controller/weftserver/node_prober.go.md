<!-- docdaemon ts:2026-10-29T20:36:07Z hash:1ee5b6f526c0ad2c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements a background prober that periodically discovers Kubernetes nodes, validates their connectivity via ephemeral jobs, and automatically provisions `WeftServer` custom resources for eligible nodes.

### Purpose
The `NodeProber` runs as a long-lived controller component to ensure every suitable node has an associated `WeftServer`. It operates on a fixed interval (`ProbeInterval`) to handle new nodes and recover from failures.

### Key Components

*   **`NodeProber` Struct**: Holds references to the Kubernetes client, scheme, and raw clientset (needed for reading pod logs).
*   **`Start`**: Initializes the probe cycle and triggers an initial cleanup of orphaned servers before entering the main ticker loop.
*   **`cleanupOrphanedServers`**: Iterates through existing `WeftServer` instances and deletes any whose target node no longer exists in the cluster. This prevents stale resources when nodes are removed.
*   **`probeNodes`**: Enumerates all nodes and delegates processing to `processNode`. Errors on individual nodes do not halt the entire batch.
*   **`processNode`**: The core logic for a single node:
    1.  Checks if a `WeftServer` named `node-<nodeName>` already exists in `weft-system`. If so, it skips.
    2.  Creates a short-lived `Job` (`weft-probe-...`) running the `weft` binary with the `probe` argument. The job is pinned to the target node using `HostNetwork: true` to access local interfaces.
    3.  Polls the job status for up to 60 seconds.
    4.  On success, reads the pod logs to extract the public IP address.
    5.  Constructs a `WeftServer` resource using the discovered IP (or fallback IPs) and a generated secret for the connection string.

### Configuration & Constraints
*   **Namespace**: All operations occur within the `weft-system` namespace.
*   **IP Priority**: Connection strings prioritize Public IP (discovered via logs), then External IP, then Internal IP.
*   **TTL**: Probe jobs have a `TTLSecondsAfterFinished` of 300 seconds to prevent accumulation of completed job artifacts.
*   **RBAC**: Requires permissions to get/list/watch Nodes and Jobs, and create/delete WeftServers.