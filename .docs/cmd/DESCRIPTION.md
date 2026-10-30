<!-- docdaemon ts:2026-10-30T03:02:25Z hash:1f9d5dc076e1700c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the entry point, build configuration, and tests for the Weft Operator binary.

## Overview
The `cmd` package bootstraps the Kubernetes controller manager, registers reconcilers for custom resources, and configures admission webhooks. It serves as the primary executable artifact, supporting both standard deployment and a node-agent mode via subcommands.

## Deep Dive
### Initialization and Preflight
The `main` function initializes a `controller-runtime` manager with leader election and health probes. Before starting controllers, it verifies that Gateway API CRDs are installed, failing fast if missing to prevent cache synchronization errors.

### Controller Reconcilers
Six reconcilers manage cluster state:
*   **WeftServer**: Mirrors external bastions from the Aquaduct TaaS API.
*   **WeftTunnel**: Manages tunnel resource lifecycle.
*   **AquaductTaaS & DNSRecord**: Handle authentication token exchange and domain registration status.
*   **WeftGateway**: Reconciles Gateway API resources, gracefully ignoring optional route kinds (TCP/TLS/UDP) if their CRDs are absent.
*   **WeftLocalDNSReflector**: Keeps public hostnames resolvable within the cluster to optimize traffic flow.

### Webhooks and Optimization
A trust injection webhook injects CA certificates into new pods, excluding the operator’s namespace and `kube-system`. The ConfigMap informer is restricted to managed resources only to prevent memory exhaustion.

## Key Interfaces
*   `main()`: Entry point for the operator binary.
*   `missingRouteKinds()`: Validates supported Gateway API route types against cluster discovery resources.

## Dependencies
*   `controller-runtime`: Kubernetes controller framework.
*   `kubernetes/client-go`: Kubernetes client libraries and fake testing dependencies.
*   Internal controllers (`aquaducttaas`, `dnsrecord`, etc.).

## Subcomponents
*   `BUILD.bazel`: Bazel build targets for compilation, packaging, and testing.
*   `main.go`: Operator initialization and controller setup.
*   `main_test.go`: Unit tests for route kind validation logic.