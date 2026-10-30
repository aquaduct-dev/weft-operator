<!-- docdaemon ts:2026-10-30T03:00:02Z hash:d9074e97f7004bf1 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `WeftServerReconciler` that manages the lifecycle of Weft server deployments, services, and persistent volume claims within a Kubernetes cluster.

The reconciler ensures that the desired state (Deployment, Service, PVC) matches the `WeftServer` custom resource specification. It parses the connection string to configure server arguments, handles image selection, and applies node affinity rules to exclude autoscaler nodes.

Key components include:
- **Resource Management**: Uses a generic `resource.Resource` helper to idempotently create or update PVCs, Deployments, and Services.
- **Client Caching**: The `weftClientFor` method caches HTTP clients per connection string to prevent connection leaks during frequent status polling.
- **Status Updates**: Scrapes tunnel statistics from the running server and updates the `WeftServer.Status`. It uses a `commitStatus` function with semantic equality checks to avoid write loops caused by changing byte counters.
- **Reconciliation Loop**: Watches for spec changes via `GenerationChangedPredicate` and requeues after `statusRefreshInterval` (30s) to keep status data fresh without triggering excessive API writes.