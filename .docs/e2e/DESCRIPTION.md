<!-- docdaemon ts:2026-10-30T03:02:27Z hash:83d292e93f704c0c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory provides the infrastructure and test suites for running end-to-end Kubernetes scenarios inside isolated Firecracker microVMs.

## Overview
The `e2e` package enables airgapped, reproducible integration testing for the weft-operator by booting k3s clusters within ephemeral virtual machines. It abstracts away environment setup through shared guest harnesses and preloaded images, allowing specific operator features to be validated against deterministic mock services or real cluster states without external network dependencies.

## Deep Dive
### Shared Infrastructure
The foundation relies on `e2ekit`, a guest-side harness that manages Kubernetes client connections, resource lifecycles, and diagnostic collection within the VM. `images` supplies static container layers and node assets (CA bundles, CRDs) injected at boot, ensuring all tests run against a consistent, offline baseline. The `defs.bzl` macros standardize the creation of these environments via `rules_firecracker`.

### Test Scenarios
Specific subdirectories validate distinct operator capabilities:
- **install**: Verifies CRD preflight logic, Helm chart lifecycle management, and operator stability during upgrades.
- **reflector**: Tests DNS reflection, TLS trust injection, and hostname claim conflicts between multiple reflectors.
- **trust**: Validates the mutating webhook’s ability to inject CA bundles into workloads while respecting namespace isolation.
- **tunnels**: Exercises HTTP, TCP, and UDP traffic routing through WeftTunnels and Gateway API integration.
- **aquaduct**: Simulates an external TaaS provider to test DNS record synchronization and token caching.

Each scenario binary acts as both the test driver and, where necessary, a fake API server, eliminating external service flakiness.

## Key Interfaces
- `weft_e2e_test`: Bazel macro wrapping `firecracker_k3s_test` with standardized image/file injection.
- `e2e_binary_image`: Constructs OCI images containing static Go binaries for guest injection.
- `Kit`: Guest-side struct managing Kubernetes clients and step execution.
- `Scenario`: Interface defining sequential test steps and lifecycle hooks.
- `DumpDiagnostics`: Collects pod logs and events upon test failure.

## Dependencies
- `rules_firecracker` (pinned via git_override)
- Kubernetes API libraries (`client-go`, `controller-runtime`)
- Helm binary for chart rendering
- Preloaded container images (k3s, pause, CoreDNS)

## Subcomponents
- `BUILD.bazel`: Public interface exposing `defs.bzl` macros.
- `defs.bzl`: Shared Bazel macros for e2e configuration.
- `aquaduct/`: AquaductTaaS and AutoDNS integration tests.
- `e2ekit/`: Guest harness and shared utilities.
- `images/`: Preloaded container images and node assets.
- `install/`: Operator installation and CRD validation tests.
- `reflector/`: DNS reflection and trust injection tests.
- `trust/`: Webhook mutation and CA bundle tests.
- `tunnels/`: Tunnel routing and Gateway controller tests.