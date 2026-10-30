<!-- docdaemon ts:2026-10-30T03:03:18Z hash:b667e0bb959539d8 model:openrouter/qwen/qwen3.7-flash -->
# Weft Operator

## TL;DR
The Weft Operator is a Kubernetes controller that automates exposing internal services to the internet via WireGuard tunnels, managing bastion nodes, traffic routing, and in-cluster DNS reflection.

## Overview
This repository contains the source code for the Weft Operator, which bridges external cloud infrastructure with Kubernetes cluster resources. It manages `WeftServer`, `WeftTunnel`, and `WeftGateway` custom resources to establish secure connectivity while optimizing intra-cluster traffic through local DNS reflection. The project uses Bazel for reproducible builds and Firecracker microVMs for airgapped end-to-end testing.

## Deep Dive
### Core Functionality
The operator reconciles desired states for tunneling and gateway configurations. It mirrors external bastions from the Aquaduct TaaS API, handles JWT authentication, and translates Gateway API routes into specific tunnel deployments. A mutating admission webhook injects CA certificates into pods to ensure trust without image modifications.

### Testing Infrastructure
End-to-end tests run inside isolated k3s clusters within Firecracker VMs. This setup validates CRD preflight logic, chart lifecycle management, and traffic routing without external network dependencies. Shared harnesses manage resource lifecycles and diagnostic collection, ensuring deterministic test outcomes.

### Build System
Bazel is the primary build system, managed via `MODULE.bazel` and Gazelle-generated rules. The configuration enforces strict dependency pinning and excludes KVM-dependent tests from standard CI runs. Pre-commit hooks automate Gazelle synchronization and unit test execution to maintain build integrity.

## Key Interfaces
- `AquaductTaaSReconciler`: Syncs bastions and domains via the aquaduct.dev API.
- `WeftGatewayReconciler`: Translates Gateway API routes into WeftTunnels.
- `WeftLocalDNSReflector`: Resolves public hostnames inside the cluster.
- `Agent`: Core loop controller for node-level DNS and TLS configuration.
- `NewCA`: Generates a fresh self-signed ECDSA P-256 CA certificate.
- `InjectPod`: Mutates a pod spec to mount the trust bundle volume.
- `Handler.Handle`: Processes admission requests to inject trust bundles.
- `Runner`: Interface for executing test scenarios and returning results.

## Dependencies
- `controller-runtime`: Kubernetes controller framework.
- `client-go`: Standard Kubernetes client libraries.
- `gateway-api`: Kubernetes Gateway API types.
- `rules_firecracker`: Pinned via git_override for e2e testing.
- Helm binary for chart rendering.

## Subcomponents
- `.github/`: GitHub Actions workflows for CI and release.
- `api/`: v1alpha1 Custom Resource Definitions.
- `chart/`: Helm chart for deployment and VPA integration.
- `cmd/`: Entry point, build configuration, and tests.
- `e2e/`: End-to-end test suites and Firecracker infrastructure.
- `internal/`: Core operator logic, controllers, and utilities.
- `scripts/`: Automation scripts for development tools.
- `tools/`: Bazel integration for controller-gen.