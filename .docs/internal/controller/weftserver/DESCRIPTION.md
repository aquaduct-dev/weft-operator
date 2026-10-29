<!-- docdaemon ts:2026-10-29T20:37:07Z hash:1aec0b939851d77a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the WeftServer controller, managing custom resources and node discovery.

## Overview
The `weftserver` package provides the Kubernetes controller logic for provisioning and monitoring WeftServers. It ensures that eligible nodes have associated server instances by probing connectivity and reconciling infrastructure resources like Deployments and Services. The controller also maintains live status metrics by polling connected servers efficiently.

## Deep Dive
### Node Probing
The `NodeProber` runs a background loop to discover new nodes and provision WeftServers where missing. It creates ephemeral probe Jobs to validate connectivity, extracts public IPs from logs, and cleans up orphaned resources when nodes are removed. Probe jobs use short TTLs to prevent artifact accumulation.

### Reconciliation
The `WeftServerReconciler` manages the lifecycle of WeftServer CRDs. For internal locations, it deploys a Deployment, Service, and PVC with specific affinity rules to avoid autoscaler nodes. External locations skip resource creation but still update status. The reconciler binds servers to `0.0.0.0` to ensure consistent network accessibility.

### Status & Caching
To maintain accurate Tx/Rx counters, the reconciler polls Weft servers every 30 seconds. A client cache prevents connection leaks by reusing HTTP transports per connection string. Status updates are optimized to skip API writes if no changes occurred, preventing watch loops that could starve the controller manager.

## Key Interfaces
*   `NodeProber`: Background component for node discovery and server provisioning.
*   `WeftServerReconciler`: Controller reconciling WeftServer custom resources.
*   `WeftClient`: Interface for interacting with Weft server APIs.

## Dependencies
*   Kubernetes API (`core/v1`, `apps/v1`, `batch/v1`)
*   controller-runtime framework
*   Ginkgo/Gomega (testing)

## Subcomponents
*   `node_prober.go`: Node discovery and provisioning logic.
*   `reconciler.go`: Main reconciliation loop and resource management.
*   `client_cache_test.go`: Tests for client reuse and leak prevention.
*   `node_prober_test.go`: Unit tests for orphan cleanup.
*   `reconciler_test.go`: Integration tests for resource lifecycle.
*   `suite_test.go`: Test environment bootstrap.