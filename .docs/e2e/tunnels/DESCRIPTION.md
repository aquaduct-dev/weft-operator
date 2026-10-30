<!-- docdaemon ts:2026-10-30T03:01:09Z hash:a7b3e3500f5af54f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the end-to-end test suite for Weft tunnels, servers, and the Gateway controller within a simulated k3s environment.

## Overview
The tests validate traffic flow (HTTP, TCP, UDP) through operator-managed pods in an airgapped microVM. They ensure correct resource reconciliation, lifecycle management, and integration with external components like the Gateway API and ACME challenges.

## Deep Dive
### Test Orchestration
`main.go` serves as the entry point, launching either the test driver or backend services (`echo`, `acme-standin`). The `tunnels_test` scenario validates full routing logic, including header filtering and fan-out, while `prober_test` isolates node discovery and server auto-creation.

### Resource Management
`kube.go` provides builders for `WeftServer` and `WeftTunnel` CRDs, along with wait-for helpers to synchronize on status conditions. It verifies that the operator correctly generates underlying Deployments, Services, and PVCs with expected labels and owner references.

### Traffic Validation
`backends.go` implements echo servers and client helpers to verify data integrity. These utilities send payloads through tunnels and assert responses match expectations, ensuring no corruption or routing errors occur during transit.

### Environment Configuration
`00-namespaces.yaml` defines isolated namespaces (`weft-system`, `e2e-gw`, `e2e-alt`) to separate control plane infrastructure from test workloads. `e2e-values.yaml` configures Helm to disable reflectors and enforce local image pulls, ensuring deterministic outcomes in the airgapped setup.

## Key Interfaces
*   `runEcho`: Starts HTTP, TCP, and UDP listeners for traffic verification.
*   `runACMEStandin`: Simulates external services for ACME challenge validation.
*   `httpVia`: Sends HTTP requests with specific headers to test tunnel routing.
*   `tcpEcho` / `udpEcho`: Verify raw byte transmission integrity over respective protocols.
*   `checkTunnelDeployment`: Asserts operator-created workload specifications.
*   `tunnelAvailable` / `serverAvailable`: Poll API server for desired status conditions.

## Dependencies
*   Kubernetes API server (via k3s)
*   Bazel build system
*   Firecracker microVM runtime
*   Helm chart rendering tools

## Subcomponents
*   `00-namespaces.yaml`: Namespace definitions for resource isolation.
*   `BUILD.bazel`: Bazel targets for compiling the e2e binary and rendering charts.
*   `backends.go`: Echo server implementations and client-side traffic helpers.
*   `e2e-values.yaml`: Helm values for airgapped, deterministic test execution.
*   `kube.go`: Kubernetes resource builders and state verification helpers.
*   `main.go`: Entry point orchestrating scenarios and backend services.
*   `prober_scenario.go`: Logic for testing NodeProber and auto-server creation.
*   `tunnels_scenario.go`: Logic for testing tunnel routing, lifecycle, and Gateway integration.