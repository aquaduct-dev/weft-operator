<!-- docdaemon ts:2026-10-30T02:59:54Z hash:d59cccffd6fb0589 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements a background controller that periodically probes Kubernetes nodes to automatically provision WeftServer resources for those with valid public connectivity.

### Purpose
The `NodeProber` runs as a manager runnable, executing periodic cycles every 3 hours. It ensures that every active node has an associated `WeftServer` resource by detecting missing servers and cleaning up orphaned ones.

### Key Components

*   **`Start`**: Initializes the prober, performs an immediate probe/cleanup cycle, then enters a loop driven by `ProbeInterval`.
*   **`cleanupOrphanedServers`**: Lists all current nodes and existing `WeftServer` resources. Deletes any `WeftServer` whose `weft.aquaduct.dev/node` label points to a node that no longer exists in the cluster.
*   **`probeNodes` & `processNode`**: Iterates through all nodes. If a `WeftServer` named `node-<nodeName>` does not exist, it creates a short-lived probe Job.
    *   The Job runs on the target node using `HostNetwork: true` to verify external reachability.
    *   Upon job success, the controller reads pod logs to extract the verified public IPv4 address.
    *   It creates a new `WeftServer` CRD instance, prioritizing the discovered public IP over internal/external node IPs for the connection string.
*   **`probedIP`**: Parses probe logs (stripping ANSI codes) to find the specific public IPv4 reported by the `weft probe` command, filtering out private or loopback addresses.

### Configuration
*   **`TargetNamespace`**: All probe Jobs and resulting `WeftServer` resources are created in this namespace.
*   **`WeftImage`**: Allows overriding the container image used for probe Jobs; defaults to `resource.DefaultWeftImage`.