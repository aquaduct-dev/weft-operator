<!-- docdaemon ts:2026-10-29T20:37:09Z hash:479fe41b446b8fdf model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the `AquaductTaaS` controller, mirroring cloud-hosted bastion servers and managing domain registrations from the aquaduct.dev API into Kubernetes.

## Overview
The `aquaducttaas` package provides two primary reconcilers: `AquaductTaaSReconciler` for syncing external bastions as `WeftServer` resources, and a DNSRecord reconciler for managing domain ownership via Cloudflare. It handles complex lifecycle management, including strict deletion ordering to prevent resource leaks and automatic JWT token rotation to maintain API connectivity.

## Deep Dive
### Bastion Mirroring
The `AquaductTaaSReconciler` polls `api.aquaduct.dev` for external servers and creates corresponding `WeftServer` objects in the cluster. It skips suspended or IP-less bastions to avoid connection failures. On deletion, it enforces a strict cleanup order: cascading deletes of dependent `DNSRecord` objects, suspending managed bastions via the API, and releasing finalizers on the access-token secret only when no other references exist. Status updates are optimized to prevent unnecessary resource version bumps that could trigger re-queue loops.

### Domain Management
The DNSRecord reconciler manages domain ownership at aquaduct.dev. It asserts write access by PATCHing records, handling errors like `ErrDomainForeign` if another user owns the record. Creation is idempotent, and resolution fetches current A-record IPs from Cloudflare. The spec’s `domainName` field is immutable, forcing delete-and-recreate for renames to allow finalizers to release the original name.

### Authentication & Client Logic
The HTTP client implements two-step authentication, exchanging a long-lived access token for a short-lived JWT via an authz endpoint. It caches the JWT with a 30-second grace period to prevent spurious 401s and automatically retries on failure to handle server-side token rotation. Connection strings use a default port of 9092 if the API response omits one.

## Key Interfaces
*   `HTTPAPIClient`: Handles HTTP interactions with the aquaduct.dev API, including JWT caching and retry logic.
*   `APIClient`: Interface abstracting API interactions for testability.
*   `AquaductTaaSReconciler`: Controller reconciling external bastions into `WeftServer` resources.
*   `DomainAPIClient`: Abstracts domain registration and ownership operations.
*   `ExternalServer`: Struct representing the state of a remote bastion server.

## Dependencies
*   `controller-runtime`: For reconciliation loops and Kubernetes object management.
*   Standard Kubernetes APIs: For CRD definitions and client-go interactions.
*   Ginkgo/Gomega: For unit and integration testing infrastructure.
*   Envtest: For isolated testing against a real Kubernetes control plane.

## Subcomponents
*   `client.go`: HTTP client implementation and data models for bastion management.
*   `client_test.go`: Tests for authentication, JWT caching, and API interactions.
*   `domain.go`: Domain model and client interface for DNSRecord management.
*   `reconciler.go`: Core logic for the AquaductTaaS reconciler.
*   `reconciler_test.go`: Test suite for reconciler lifecycle and error handling.
*   `suite_test.go`: Integration test environment setup using envtest.
*   `BUILD.bazel`: Bazel build targets for library, tests, and source aggregation.