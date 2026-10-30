<!-- docdaemon ts:2026-10-30T03:01:20Z hash:c390a72a96001b66 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `e2ekit` package provides the guest-side harness and shared utilities for running end-to-end Kubernetes scenarios inside Firecracker microVMs.

## Overview
This directory contains the core infrastructure for executing e2e tests within a constrained virtualized environment. It abstracts cluster interactions, resource lifecycle management, and diagnostic collection, allowing test suites to focus on scenario logic rather than environment setup. The codebase was refactored to leverage upstream `rules_firecracker` tooling, ensuring consistent kernel netfilter options required by components like kube-proxy.

## Deep Dive
### Guest Harness Execution
The `Main` function serves as the entry point, parsing flags and initializing a `Kit` instance. This kit manages the connection to the k3s cluster and orchestrates sequential step execution. Failures trigger immediate diagnostic dumps, including pod logs and events, before writing a JSON report via `clusterharness`.

### Cluster Interaction
The `Kit` struct encapsulates Kubernetes clients (`client.Client`, `*kubernetes.Clientset`) and exposes helper methods such as `Kubectl`, `Apply`, and `Delete`. These abstractions simplify resource manipulation during tests, reducing boilerplate in individual test cases.

### Operator Lifecycle Management
`InstallOperator` handles the deployment of the Weft operator, managing CRD application and webhook readiness checks. This ensures that dependent operators are fully operational before subsequent test steps proceed, preventing race conditions.

### Diagnostics and Inspection
When a step fails, `DumpDiagnostics` collects pod states, warning events, and log tails to aid debugging. Additionally, functions like `ContainerPID` and `EstablishedConns` allow direct inspection of container processes and network state from within the VM, providing granular visibility into the guest environment.

## Key Interfaces
- `Kit`: Holds Kubernetes clients and configuration, providing helper methods for cluster interaction.
- `Step`: Defines a single unit of work within a scenario.
- `Scenario`: A collection of steps executed sequentially.
- `WaitFor`: Utility function for polling until a condition is met.
- `Holds`: Utility function for verifying a condition persists over time.
- `DumpDiagnostics`: Collects pod states, events, and logs for failure analysis.
- `countEstablished`: Parses TCP state data to count established connections.
- `Tail`: Retrieves the last N lines from a newline-delimited string.

## Dependencies
- Kubernetes API types (`admissionregistration`, `apps`, `core`, `meta`)
- client-go libraries
- controller-runtime
- gateway-api clients
- rules_firecracker (pinned via git_override)

## Subcomponents
- `BUILD.bazel`: Bazel build targets for the Go library and tests.
- `e2ekit.go`: Main guest harness implementation and utility functions.
- `e2ekit_test.go`: Unit tests for helper functions like connection counting and string tailing.