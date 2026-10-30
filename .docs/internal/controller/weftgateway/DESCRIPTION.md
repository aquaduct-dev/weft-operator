<!-- docdaemon ts:2026-10-30T03:00:11Z hash:f70c6a719e376bc6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `weftgateway` controller reconciles Kubernetes Gateway API resources into WeftTunnels and DNSRecords.

## Overview
This directory implements the core logic for translating standard Gateway API constructs into the internal Weft infrastructure. It watches Gateways, Routes, and AquaductTaaS objects to automatically provision tunnels for backend services and generate corresponding DNS records. The controller handles protocol-specific routing strategies, ensuring Layer 7 traffic fans out across bastions while Layer 4 traffic is pinned to a single bastion to prevent port collisions.

## Deep Dive
### Reconciliation Logic
The `WeftGatewayReconciler` drives the primary control loop. It filters incoming routes against Gateway listeners, respecting section names and hostnames to ensure precise attachment. Upon detecting changes, it creates or updates `WeftTunnel` Custom Resources for each matched backend service. A finalizer ensures cleanup of stale tunnels and DNS records when a Gateway is deleted.

### Protocol Handling
The controller distinguishes between Layer 7 (HTTP/HTTPS/TLS) and Layer 4 (TCP/UDP) protocols. For L7, tunnels fan out to all non-suspended bastions, allowing the proxy to demux traffic by Host header or SNI. For L4, the controller pins traffic to a single eligible bastion using deterministic selection logic to avoid IP/port conflicts. URL schemes are constructed accordingly (`http://`, `https://`, `tcp://`, `udp://`).

### Filter Translation
HTTP route filters, specifically `RequestHeaderModifier`, are translated into URL fragments appended to tunnel source URLs. This allows the tunnel to rewrite headers (e.g., adding `X-Forwarded-Proto`) before forwarding requests to backends, preserving scheme information for applications relying on absolute URLs.

### DNS Generation
For every unique listener hostname in a Gateway, the controller automatically emits a `DNSRecord`. These records are named deterministically based on the gateway identity and hostname hash, and they reside in the same namespace as the associated `AquaductTaaS` resource.

## Key Interfaces
- `WeftGatewayReconciler`: Main reconciler struct implementing `reconcile.Reconciler`.
- `httpFiltersToFragment`: Converts HTTPRoute filters into URL-encoded fragment strings.
- `parentRefsSelectListener`: Determines if a specific listener is selected by parent references.
- `classifyListenerHostnames`: Categorizes hostnames into L7 or L4 groups.
- `pickL4Bastion`: Selects a single bastion for L4 traffic pinning.

## Dependencies
- `controller-runtime`: Core reconciliation framework and client interaction.
- `gateway-api`: Standard Kubernetes Gateway API types (v1, v1alpha2).
- `internal/resource`: Internal resource management utilities.
- `api/v1alpha1`: Custom Resource Definitions for WeftTunnel and DNSRecord.
- `k8s.io/apimachinery`: Standard Kubernetes error handling and runtime objects.

## Subcomponents
- `reconciler.go`: Main controller implementation.
- `fragment_test.go`: Unit tests for filter serialization and listener selection.
- `helpers_test.go`: Unit tests for hostname classification and bastion picking.
- `reconciler_test.go`: Integration tests for full reconciliation cycles.
- `suite_test.go`: Ginkgo test suite setup and environment initialization.
- `BUILD.bazel`: Bazel build targets and dependencies.