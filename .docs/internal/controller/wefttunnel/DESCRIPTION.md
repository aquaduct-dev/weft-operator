<!-- docdaemon ts:2026-10-29T20:37:07Z hash:41690945ff7822c5 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `wefttunnel` package implements the controller responsible for establishing and managing network tunnels between Kubernetes namespaces.

## Overview
This directory contains the reconciliation logic for `WeftTunnel` custom resources, ensuring connectivity to `WeftServer` targets via dedicated tunnel agent Deployments. It handles lifecycle management, target resolution across namespaces, and status synchronization to reflect tunnel availability.

## Deep Dive
### Reconciliation Logic
The `Reconcile` function iterates over target `WeftServer` objects, creating or updating a unique Deployment per server named `tunnel-<tunnel>-to-<server>`. It uses `controllerutil.CreateOrUpdate` with careful container mutation to preserve API-server-defaulted fields like termination messages, preventing infinite update loops caused by diffing on immutable defaults.

### Target Selection and Filtering
Tunnels with empty `TargetServers` connect to all local servers plus those in `weft-system`. Explicit targets are resolved within the tunnel's namespace. The controller watches both `WeftTunnel` and `WeftServer` resources, using `GenerationChangedPredicate` to filter out status-only updates and avoid unnecessary reconciliations.

### Status Updates
The `updateStatus` function checks if associated Deployments have `ReadyReplicas > 0`. It sets the `Available` condition on the `WeftTunnel` status directly based on `ReadyReplicas` rather than `AvailableReplicas`, supported by setting `MinReadySeconds: 15` on the Deployment to stabilize flapping pods.

## Key Interfaces
*   `WeftTunnelReconciler`: Main controller struct implementing the reconcile loop.
*   `SetupWithManager`: Configures the controller to watch relevant resources.
*   `findTunnelsForServer`: Maps server events to relevant tunnel requests.

## Dependencies
*   Kubernetes API types (`apps/v1`, `core/v1`)
*   `controller-runtime` (client, metrics, predicates)
*   Ginkgo/Gomega for testing infrastructure

## Subcomponents
*   `reconciler.go`: Core controller implementation.
*   `suite_test.go`: Test environment bootstrap.
*   `reconciler_test.go`: Integration tests for deployment lifecycle.
*   `reconciler_idempotency_test.go`: Tests for update stability.
*   `reconciler_status_test.go`: Tests for status propagation.