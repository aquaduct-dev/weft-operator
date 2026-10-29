<!-- docdaemon ts:2026-10-29T20:36:12Z hash:81fff874d3f9b91e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the Kubernetes controller that manages WeftServer resources, ensuring their underlying infrastructure and live status are reconciled.

The `WeftServerReconciler` owns the reconciliation loop for `weftv1alpha1.WeftServer` objects. It creates or updates a Deployment, Service, and PersistentVolumeClaim (PVC) when the server location is internal, handling configuration parsing from the ConnectionString. For external locations, it skips resource creation but continues to update status.

Key behaviors include:
*   **Resource Management**: Uses a helper `resource.Resource` to declaratively manage PVCs, Deployments, and Services, applying owner references and specific affinity rules to exclude autoscaler nodes.
*   **Status Reconciliation**: Periodically polls the Weft server via `ListTunnels` to update live Tx/Rx counters in the CRD status. This runs on a 30-second interval (`statusRefreshInterval`) rather than triggering on every write to avoid hot loops.
*   **Client Caching**: Maintains a cache of `WeftClient` instances keyed by connection string to prevent leaking HTTP connections and goroutines during frequent status refreshes.
*   **Commit Optimization**: The `commitStatus` method compares the new status against a snapshot taken at the start of the pass, skipping API writes if no changes occurred to reduce API server load.