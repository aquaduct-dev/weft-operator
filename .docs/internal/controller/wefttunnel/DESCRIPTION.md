<!-- docdaemon ts:2026-10-30T03:00:10Z hash:0f81bdcb54eb0337 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `wefttunnel` package implements the controller logic for managing one-to-one tunnel deployments between WeftTunnel resources and target WeftServers.

## Overview
This directory contains the reconciliation loop that ensures Kubernetes Deployments exist for each configured tunnel endpoint. It handles resource creation, pruning of obsolete tunnels, and status propagation to reflect the health of underlying pods. The controller is designed to be idempotent, avoiding unnecessary API writes by carefully managing container field mutations.

## Deep Dive
### Reconciliation Loop
The `WeftTunnelReconciler` watches for changes in `WeftServer` objects. When a server changes, the mapper enqueues all relevant tunnels. The reconciler resolves target servers (either explicit or all in namespace) and constructs tunnel URLs. It uses `controllerutil.CreateOrUpdate` to maintain Deployment resources, ensuring they match the desired spec.

### Idempotency and Mutation
To prevent infinite update loops caused by API server defaults, the reconciler mutates existing container fields in-place via `containerByName`. This preserves fields like `TerminationMessagePath` which would otherwise be stripped and restored on every reconcile cycle. Readiness is determined by `ReadyReplicas` rather than `AvailableReplicas`, combined with a `MinReadySeconds` of 15s to filter out transient pod flapping.

### Pruning and Status
The controller lists existing deployments labeled with the tunnel name and deletes any not currently expected, preventing orphaned resources. A deferred `updateStatus` function checks if all associated deployments have `ReadyReplicas > 0`, setting the `Available` condition accordingly. This ensures the Custom Resource status accurately mirrors the state of its backing infrastructure.

## Key Interfaces
- `WeftTunnelReconciler`: Main struct implementing the reconciliation loop.
- `SetupWithManager`: Configures the controller to watch `WeftServer` objects.
- `findTunnelsForServer`: Mapper function that enqueues tunnels based on server matching.

## Dependencies
- `aquaduct.dev/weft-operator/api/v1alpha1`: Custom resource definitions.
- `aquaduct.dev/weft-operator/internal/resource`: Shared CRUD logic.
- `sigs.k8s.io/controller-runtime`: Controller framework and client interfaces.
- `k8s.io/api/apps/v1`: Deployment APIs.
- `k8s.io/api/core/v1`: Pod and Service APIs.

## Subcomponents
- `reconciler.go`: Core controller implementation.
- `reconciler_test.go`: Integration tests for deployment lifecycle and targeting.
- `reconciler_idempotency_test.go`: Tests for update convergence and default preservation.
- `reconciler_status_test.go`: Tests for status condition propagation.
- `suite_test.go`: Test environment bootstrap using envtest.
- `BUILD.bazel`: Bazel build configuration for library and tests.