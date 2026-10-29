<!-- docdaemon ts:2026-10-29T20:36:21Z hash:bc39f58e40d1bfa9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `WeftTunnel` controller, which manages lifecycle and connectivity for tunnels between Kubernetes namespaces.

### Purpose
The `WeftTunnelReconciler` ensures that a tunnel exists between a source URL (`SrcURL`) and destination URL (`DstURL`) for one or more target servers. It creates a dedicated Deployment per target server to run the tunnel agent.

### Key Functions

*   **`Reconcile`**: The main loop. It identifies target `WeftServer` objects (either explicitly listed in `Spec.TargetServers` or all servers in the namespace plus `weft-system`). For each server, it constructs a unique Deployment named `tunnel-<tunnel>-to-<server>`. It uses `controllerutil.CreateOrUpdate` to ensure the deployment matches the desired spec, including the correct container image and arguments. Finally, it prunes any existing deployments owned by this tunnel that are no longer needed.
*   **`updateStatus`**: Deferred at the end of `Reconcile`, this function checks if all associated tunnel Deployments have `ReadyReplicas > 0`. It sets the `Available` condition on the `WeftTunnel` status accordingly.
*   **`SetupWithManager`**: Configures the controller to watch `WeftTunnel` resources and `WeftServer` resources. It filters `WeftServer` updates using `GenerationChangedPredicate` to avoid unnecessary reconciliations triggered by status-only changes (like traffic counters).
*   **`findTunnelsForServer`**: Maps `WeftServer` events to relevant `WeftTunnel` requests. It mirrors the logic in `Reconcile` to determine which tunnels should be re-enqueued when a server changes.

### Implementation Details
*   **Container Mutation**: The code carefully mutates existing containers in place via `containerByName` rather than replacing the slice. This prevents accidental loss of API-server-defaulted fields (e.g., resource limits) which would cause endless reconciliation loops.
*   **Readiness**: Uses `ReadyReplicas` directly instead of `AvailableReplicas` for immediate feedback, supported by setting `MinReadySeconds: 15` on the Deployment.
*   **Target Selection**: Tunnels with empty `TargetServers` connect to all local servers and those in `weft-system`. Explicit targets are resolved within the tunnel's own namespace.