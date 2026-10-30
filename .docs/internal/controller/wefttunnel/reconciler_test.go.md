<!-- docdaemon ts:2026-10-30T02:59:56Z hash:c6e593ee7870c990 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains integration tests for the `WeftTunnel` controller, verifying deployment lifecycle and server targeting logic.

The test suite validates that `WeftTunnelReconciler` correctly manages Kubernetes Deployments based on `TargetServers`. It covers four key scenarios:
- **Empty Target List**: Creates deployments for all available `WeftServer` resources, ensuring internal servers use their connection strings and external servers remain unchanged.
- **Specific Targets**: Creates deployments only for explicitly listed servers, ignoring others.
- **Pruning**: Deletes obsolete deployments when the target list changes, ensuring only current targets have active pods.
- **Cross-Namespace**: Handles servers in different namespaces (e.g., `weft-system`) from the tunnel's namespace.

Tests use Ginkgo/Gomega with a fake Kubernetes client to assert Deployment creation, argument construction (URLs, flags), and deletion. The `WeftImage` is pinned to ensure consistent container images during reconciliation checks.