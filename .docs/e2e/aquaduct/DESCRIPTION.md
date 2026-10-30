<!-- docdaemon ts:2026-10-30T03:01:26Z hash:da7cbaf8989fbd1f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the end-to-end test suite for the AquaductTaaS operator, validating integration with a simulated external service.

## Overview
The `aquaduct` package provides a comprehensive testing framework that runs inside a Firecracker microVM alongside a k3s cluster. It verifies that the weft-operator correctly manages TaaS lifecycles, DNS records, and Gateway AutoDNS synchronization against a deterministic, in-memory mock of the aquaduct.dev API. This setup ensures reliable, airgapped validation of complex edge cases like token caching, conflict resolution, and deletion ordering without external network dependencies.

## Deep Dive
### Test Orchestration
The `aquaducte2e` binary serves dual purposes: it acts as the test driver orchestrating scenarios and as the fake API server itself. Built as a static Go executable, it avoids libc dependencies required by the microVM environment. The `scenario` struct defines the lifecycle steps, including operator installation, bastion deployment, and verification of state transitions.

### Fake API Implementation
The `fake` struct implements an in-memory HTTP server mimicking the real aquaduct.dev service. It handles authentication via JWT minting and validates access tokens. The `/api/*` endpoints replicate production behavior, including ownership checks and fault injection, while `/_fake/*` endpoints provide introspection tools for tests to reset state or verify request logs. This ensures protocol compatibility between the operator’s client and the mock server.

### Scenario Validation
Specific scenario files (`scenario_dns.go`, `scenario_gateway.go`) validate distinct operational aspects. DNS tests confirm correct registration sequences, handling of foreign domain conflicts (403), and contested writes (409). Gateway tests verify that DNS records are created, updated, and pruned in sync with Listener changes, enforcing L4 hostname constraints. Deletion order is strictly checked to ensure domains are unregistered before bastions are suspended, preventing orphaned resources.

## Key Interfaces
- `fake`: In-memory HTTP server implementing the aquaduct.dev API contract.
- `scenario`: Struct defining the e2e test lifecycle and step execution logic.
- `aquaducte2e`: Binary entry point supporting both test runner and fake server modes.
- `checkRegistered`: Helper validating specific HTTP request sequences against recorded logs.

## Dependencies
- Kubernetes API types and controller-runtime clients for in-cluster state management.
- `weft_e2e_test` rule for booting the Firecracker VM environment.
- Helm chart configuration (`e2e-values.yaml`) for operator deployment settings.

## Subcomponents
- `BUILD.bazel`: Bazel build targets for library, binary, and test rules.
- `e2e-values.yaml`: Helm values configuring the operator for the test environment.
- `fake.go`: Core implementation of the mock API server.
- `fake_test.go`: Unit tests validating the fake server’s protocol compliance.
- `helpers.go`: Utilities for Kubernetes resource interaction and API control.
- `main.go`: Entry point dispatching between scenario and fake modes.
- `scenario.go`: Main test orchestration and lifecycle definition.
- `scenario_dns.go`: DNS record lifecycle and conflict resolution tests.
- `scenario_gateway.go`: Gateway API and AutoDNS synchronization tests.