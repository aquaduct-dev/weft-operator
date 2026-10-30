<!-- docdaemon ts:2026-10-30T02:59:58Z hash:bd85af4ffbb111bf model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `WeftTunnel` controller, which manages one-to-one tunnel deployments between a WeftTunnel resource and target WeftServers.

### Key Components

*   **`WeftTunnelReconciler`**: The main reconciliation loop. It resolves target servers (either explicit or all in namespace/weft-system), constructs tunnel URLs, and ensures corresponding `Deployment` resources exist.
*   **Deployment Management**: Uses `controllerutil.CreateOrUpdate` to maintain tunnel pods. It employs `SetControllerReference` so that Deployment readiness events trigger re-reconciliation of the parent WeftTunnel.
*   **Pruning**: Lists existing deployments labeled with the tunnel name and deletes any not currently expected, ensuring no orphaned resources remain.
*   **Status Updates**: The deferred `updateStatus` function checks if all associated deployments have `ReadyReplicas > 0`, setting the `Available` condition accordingly.
*   **Watch Logic**: `SetupWithManager` watches for changes to `WeftServer` objects. The `findTunnelsForServer` mapper enqueues relevant tunnels based on namespace and target server matching logic, using `GenerationChangedPredicate` to avoid churn from status-only updates.

### Implementation Notes

*   **Container Mutation**: The code mutates existing container fields in-place via `containerByName` rather than replacing the slice. This prevents unnecessary API writes caused by API server defaults being stripped and restored on every reconcile cycle.
*   **Readiness Signal**: Readiness is determined by `ReadyReplicas` rather than `AvailableReplicas`, combined with a `MinReadySeconds` of 15s on the Deployment to filter out transient pod flapping.