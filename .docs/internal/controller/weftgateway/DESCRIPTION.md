<!-- docdaemon ts:2026-10-29T20:37:09Z hash:479245c4a8abfbf5 model:openrouter/qwen/qwen3.7-flash -->
# Weft Gateway Controller

## TL;DR
This directory implements the Kubernetes controller that reconciles Gateway API resources into WeftTunnels and DNSRecords.

## Overview
The `weftgateway` package provides a controller that watches Gateway, GatewayClass, and HTTPRoute/TCPRoute/TLSRoute/UDPRoute resources. It translates these standard Kubernetes networking constructs into internal `WeftTunnel` custom resources and external `DNSRecord` objects, enabling traffic routing through the Weft infrastructure. The controller ensures strict listener-route attachment to prevent proxy conflicts and handles protocol-specific logic for Layer 4 pinning versus Layer 7 fan-out.

## Deep Dive
### Reconciliation Logic
The `WeftGatewayReconciler` acts as the central control loop. It validates ownership via `GatewayClass` and iterates over attached routes. A critical fix in recent history addressed a cross-product bug where every route was incorrectly attached to every listener; the current implementation uses `isRouteAttachedToListener` to ensure one-to-one mapping between specific routes and listeners. This prevents duplicate tunnel creation and associated proxy conflicts.

### Protocol Handling and Bastion Pinning
The controller supports HTTP, HTTPS, TLS, TCP, and UDP protocols. For Layer 7 (HTTP/HTTPS/TLS), hostnames fan out to all eligible bastions, allowing load balancing across multiple nodes. In contrast, Layer 4 (TCP/UDP) hostnames are pinned to a single bastion using `pickL4Bastion`. This deterministic selection avoids port collisions on shared bastion IPs. If no non-suspended bastion is available for an L4 hostname, no DNS record is created to prevent silent misrouting.

### Header Rewriting and DNS Management
The reconciler translates `RequestHeaderModifier` filters from HTTPRoutes into URL fragments within the tunnel configuration, ensuring correct scheme handling (e.g., preserving HTTPS semantics behind terminating proxies). Additionally, it automatically generates `DNSRecord` CRDs for each unique listener hostname. These records are named deterministically based on the gateway namespace/name and hostname hash, and are pruned upon gateway deletion via finalizers.

## Key Interfaces
- `WeftGatewayReconciler`: The main controller struct implementing reconcile logic.
- `classifyListenerHostnames`: Helper to categorize hostnames by protocol layer.
- `pickL4Bastion`: Deterministic function to select a single bastion for L4 traffic.
- `isRouteAttachedToListener`: Validation helper to match routes to specific listeners.

## Dependencies
- `controller-runtime`: Kubernetes controller framework.
- `gateway-api`: Standard Gateway API types and clients.
- `envtest`: Local test environment setup for integration tests.
- `Ginkgo/Gomega`: Testing frameworks for suite execution.

## Subcomponents
- `reconciler.go`: Core reconciliation logic for Gateways and Routes.
- `helpers_test.go`: Unit tests for hostname classification and bastion picking.
- `reconciler_test.go`: Integration tests for tunnel and DNS record creation.
- `suite_test.go`: Ginkgo suite setup and local cluster bootstrapping.
- `BUILD.bazel`: Bazel build targets for library and test suites.