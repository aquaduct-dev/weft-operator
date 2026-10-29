<!-- docdaemon ts:2026-10-29T20:37:08Z hash:304b6351eb3f88ba model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `dnsrecord` controller reconciles Kubernetes Custom Resources with the AquaductTaaS API to manage domain registrations and DNS resolution.

## Overview
This directory implements the `DNSRecord` controller, which ensures that specified domains at `aquaduct.dev` are registered, owned, and correctly resolved to bastion host IPs. It coordinates with the `aquaducttaas` controller to fetch credentials and bastion lists, driving the external state toward the desired configuration defined in the CRD. The system handles lifecycle events including initial registration, ownership assertion via PATCH, and cleanup on deletion.

## Deep Dive
### Reconciliation Logic
The `DNSRecordReconciler` drives domains to a desired state by resolving dependencies and computing expected IP sets from bastion nodes. It uses PUT for new domains and PATCH for updates, asserting ownership through minimal requests authenticated by JWT. To prevent infinite reconcile loops caused by status updates triggering watches, the controller compares sorted outputs (`ResolvedIPs`, `selectBastions`) against previous states, writing `.status` only when semantic changes occur or the heartbeat is stale.

### Ownership and Error Handling
The controller gracefully handles pre-existing domains. If a GET returns 200, it issues a PATCH to assert ownership; failures result in `ErrDomainForeign`, marking records as externally managed if DNS matches expectations. Bastion selection filters suspended nodes and validates target IDs, ensuring deterministic IP sets. The `unregister-on-delete` finalizer ensures cleanup calls `DeleteDomain` before removal.

### Status Conditions
Health is tracked via three conditions: **Registered** (claim/update success), **Resolved** (DNS A-records match expected IPs), and **Ready** (end-to-end correctness). These provide clear signals for fleet-wide monitoring without over-triggering watch loops.

## Key Interfaces
*   `DNSRecordReconciler`: Main controller struct implementing the reconciliation loop.
*   `mockDomainClient`: Test helper implementing the external API interface.
*   `envtest.Environment`: Setup for local Kubernetes control plane simulation.

## Dependencies
*   `controller-runtime`: Kubernetes operator framework for watch loops and client management.
*   `v1alpha1`: API types defining the `DNSRecord` Custom Resource Definition.
*   `aquaducttaas`: Controller providing credentials and bastion list data.
*   `Ginkgo/Gomega`: Testing framework for integration tests.
*   `envtest`: Tool for simulating a cluster environment with etcd and API server.

## Subcomponents
*   `BUILD.bazel`: Bazel build targets for library and tests.
*   `reconciler.go`: Core controller logic and status management.
*   `reconciler_test.go`: Integration tests for lifecycle and error scenarios.
*   `suite_test.go`: Test environment setup and teardown.