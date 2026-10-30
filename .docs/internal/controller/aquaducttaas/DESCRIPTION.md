<!-- docdaemon ts:2026-10-30T03:00:10Z hash:169d2a98466d197e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the `AquaductTaaS` controller, which mirrors external bastion servers from the aquaduct.dev API into the cluster as `WeftServer` resources.

## Overview
The `aquaducttaas` package bridges the gap between cloud-hosted infrastructure and Kubernetes-native resource management. It authenticates with the Aquaduct.dev API using a two-step JWT flow, synchronizes bastion states to local `WeftServer` Custom Resources, and manages domain registrations via `DNSRecord` CRs. The controller ensures safe lifecycle management, including strict deletion ordering to prevent orphaned resources or wedged finalizers.

## Deep Dive
### Synchronization and Mirroring
The `AquaductTaaSReconciler` polls the `/api/bastion` endpoint to maintain a desired state of `WeftServer` objects. It uses labels to track ownership and prunes stale entries. To avoid watch loops caused by unnecessary status updates, the reconciler only writes to the status subresource when actual state changes or the sync heartbeat becomes stale. Suspended bastions or those without IPs are skipped to prevent downstream tunnel pods from crashing.

### Authentication and Client Management
The `HTTPAPIClient` handles two-step authentication, exchanging long-lived access tokens for short-lived JWTs. It caches these tokens with a grace period to prevent expiry races and automatically retries on 401 responses. Context values allow per-request endpoint overrides, enabling multiple `AquaductTaaS` resources to share a single client instance safely.

### Domain Registration and Ownership
The `DomainAPIClient` manages DNS records at aquaduct.dev. It uses `PUT` for registration and `PATCH` to assert ownership, surfacing conflicts (409) or foreign ownership (403/401) as specific errors rather than retrying indefinitely. This allows the reconciler to set appropriate status conditions for domains that cannot be claimed.

### Deletion Safety
Deletion logic is strictly ordered: dependent `DNSRecord` objects are cascaded-deleted first, then remote bastions are suspended to stop billing, and finally, the access-token Secret pin is released. A secondary finalizer on the token Secret prevents deletion while any `AquaductTaaS` in the namespace still references it, avoiding cleanup wedges.

## Key Interfaces
- `APIClient`: Interface for interacting with the aquaduct.dev bastion API.
- `DomainAPIClient`: Interface for managing domain registrations and DNS records.
- `AquaductTaaSReconciler`: Controller-runtime reconciler for syncing bastions and domains.
- `ExternalServer`: Domain model representing a bastion server.
- `Domain`: Domain model representing a registered DNS record.

## Dependencies
- `controller-runtime`: Kubernetes controller framework.
- `client-go`: Standard Kubernetes client libraries.
- `envtest`: Testing environment for integration tests.
- `Ginkgo/Gomega`: BDD testing framework.

## Subcomponents
- `client.go`: HTTP client implementation for bastion operations.
- `domain.go`: Domain model and client for DNS record management.
- `reconciler.go`: Main controller logic for synchronization and lifecycle.
- `client_test.go`: Unit tests for the HTTP client.
- `endpoint_test.go`: Tests for endpoint overrides and response handling.
- `reconciler_test.go`: Ginkgo suite for reconciler lifecycle and cleanup.
- `suite_test.go`: Integration test setup and environment initialization.