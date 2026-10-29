<!-- docdaemon ts:2026-10-29T20:38:48Z hash:64d888cdbcb5e992 model:openrouter/qwen/qwen3.7-flash -->
# e2e/reflector

## TL;DR
This directory contains the end-to-end test suite for the WeftLocalDNSReflector, validating in-cluster traffic routing and DNS trust establishment within an isolated Firecracker microVM environment.

## Overview
The reflector tests ensure that the operator correctly manages DNS reflection, TLS certificate injection, and service discovery without relying on external networks. By booting a single-node k3s cluster inside a microVM with preloaded images, the suite verifies that traffic to published registries remains strictly within the cluster boundaries. It covers both manual reflector configurations and the automatic creation of a cluster-wide default reflector.

## Deep Dive
### Test Orchestration
The `main.go` binary acts as the guest-side orchestrator, supporting three modes: running scenario steps, simulating client workloads, or maintaining container liveness. Scenarios are defined in `scenario.go` and `default_scenario.go`, which orchestrate the installation of the operator, deployment of backend services, and validation of DNS resolution. The `allHostnames` mode allows a single reflector to serve all tunnelled registries, simplifying cluster-wide testing.

### Client Validation
The `client.go` file implements the workload that validates the reflector’s behavior. It performs DNS lookups to verify resolution against expected IPs and issues HTTPS requests using only the system trust store. This ensures the trust webhook correctly populated the CA bundle. If the push flag is set, it uploads a minimal OCI image to exercise the full push workflow under the reflected identity.

### Kubernetes Helpers
`kube.go` provides utilities for managing pod lifecycles and retrieving logs within the test environment. Functions like `waitFor` and `kubectlApply` abstract common operations, ensuring deterministic state checks and safe application of large CRDs. The `runClientPod` function handles the complete lifecycle of test clients, including cleanup and log retrieval upon failure.

### Configuration
`e2e-values.yaml` configures the Helm chart for an airgapped environment, disabling network pulls and autoscaling features irrelevant to the microVM setup. `00-namespaces.yaml` defines the strict separation of concerns between the operator, backend services, and client workloads.

## Key Interfaces
- `runScenario`: Orchestrates the test lifecycle and timeout context for scenario execution.
- `runClient`: Entry point for the client workload, validating DNS and TLS trust.
- `checkDefaultCreated`: Verifies the existence and readiness of the cluster-default reflector.
- `waitFor`: Generic polling loop for checking state changes with progress logging.
- `runClientPod`: Manages the creation, execution, and cleanup of test client pods.

## Dependencies
- Kubernetes API types for resource management.
- Internal cluster harness utilities for VM orchestration.
- Firecracker microVM runtime for isolated testing.
- Helm chart templates for operator configuration.

## Subcomponents
- `00-namespaces.yaml`: Namespace definitions for isolation.
- `BUILD.bazel`: Bazel build targets for test binaries and images.
- `client.go`: E2E client workload implementation.
- `default_scenario.go`: Tests for the automatic cluster-default reflector.
- `e2e-values.yaml`: Helm values for airgapped testing.
- `kube.go`: Kubernetes helper utilities.
- `main.go`: Guest-side orchestrator binary.
- `no-default-reflector.yaml`: Configuration to disable default reflector.
- `scenario.go`: Manual reflector test scenario definition.