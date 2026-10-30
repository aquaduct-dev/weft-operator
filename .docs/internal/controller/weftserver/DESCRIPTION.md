<!-- docdaemon ts:2026-10-30T03:00:10Z hash:643ccc832df01fbd model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `weftserver` controller manages the lifecycle of WeftServer resources, ensuring correct deployment and status synchronization.

## Overview
This package implements the Kubernetes controller for `WeftServer` custom resources. It handles the creation and maintenance of underlying infrastructure, including Deployments, Services, and PersistentVolumeClaims, while actively polling server metrics to update resource status. Additionally, it includes a background node prober that automatically discovers nodes with public connectivity and provisions corresponding servers.

## Deep Dive

### Reconciliation Logic
The `WeftServerReconciler` watches for changes in the `WeftServer` spec. Upon reconciliation, it parses connection strings to configure server arguments and applies node affinity rules to avoid scheduling on ephemeral autoscaler nodes. It uses a generic resource helper to idempotently manage PVCs, Deployments, and Services. To prevent write-loops caused by changing byte counters in status updates, the reconciler employs semantic equality checks before committing status changes.

### Client Caching
Historically, frequent status polling created new HTTP transports per cycle, leading to connection leaks. The reconciler now caches one `WeftClient` per connection string. This optimization prevents memory exhaustion and goroutine leaks by reusing existing TLS connections during periodic status refreshes.

### Node Probing
The `NodeProber` runs as a manager runnable every three hours. It detects nodes lacking an associated `WeftServer` and creates short-lived probe Jobs using `HostNetwork`. These jobs verify external reachability and extract public IPv4 addresses from logs. If successful, a new `WeftServer` CRD is created. The prober also cleans up orphaned servers linked to deleted nodes.

## Key Interfaces
*   `WeftServerReconciler`: Main controller struct managing WeftServer resources.
*   `NodeProber`: Background runnable for automatic server provisioning based on node availability.
*   `WeftClient`: Interface for communicating with running Weft servers to scrape tunnel statistics.

## Dependencies
*   `k8s.io/api/core/v1`, `apps/v1`, `batch/v1`: Kubernetes API types for Pods, Deployments, and Jobs.
*   `controller-runtime`: Framework for building controllers and managing the reconciliation loop.
*   `internal/resource`: Helper utilities for idempotent resource creation and updates.
*   `api/v1alpha1`: Definitions for the `WeftServer` Custom Resource Definition.

## Subcomponents
*   `reconciler.go`: Core reconciliation logic for WeftServer resources.
*   `node_prober.go`: Background job for discovering nodes and auto-provisioning servers.
*   `client_cache_test.go`: Tests for client caching mechanisms to prevent leaks.
*   `reconciler_test.go`: Integration tests for the main reconciliation loop.
*   `node_prober_test.go`: Unit tests for orphan cleanup and node probing logic.
*   `probed_ip_test.go`: Tests for parsing public IPs from probe logs.
*   `suite_test.go`: Test suite bootstrap using envtest.