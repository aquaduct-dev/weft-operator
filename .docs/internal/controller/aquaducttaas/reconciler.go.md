<!-- docdaemon ts:2026-10-29T20:36:07Z hash:6a28e3c47d558820 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `AquaductTaaSReconciler`, which mirrors cloud-hosted bastions from the aquaduct.dev API into Kubernetes as `WeftServer` resources.

The reconciler polls `api.aquaduct.dev` for external servers and creates or updates corresponding `WeftServer` objects in the cluster, labeling them with `ownerLabel` for lifecycle management. It skips suspended or IP-less bastions to prevent connection failures. On deletion, it enforces a strict cleanup order: cascading deletes of dependent `DNSRecord` objects, suspending all managed bastions via the API, and releasing a finalizer on the access-token secret only when no other `AquaductTaaS` references it.

Key functions include:
- `Reconcile`: Orchestrates the sync loop, handling token retrieval, API calls, and status updates.
- `syncWeftServers`: Mirrors the desired state of bastions, pruning stale `WeftServer` instances.
- `handleDeletion`: Manages finalizer logic to ensure clean teardown of cloud resources and secrets.
- `commitStatus`: Optimizes status writes by comparing against previous states to avoid unnecessary resource version bumps and watch loops.