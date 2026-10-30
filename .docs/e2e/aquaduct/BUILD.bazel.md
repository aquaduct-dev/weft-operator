<!-- docdaemon ts:2026-10-30T02:59:27Z hash:378f02eab9b252ae model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the Aquaduct end-to-end test suite, which validates the operator's integration with a simulated Aquaduct.dev service.

The `aquaduct_lib` target compiles the shared Go code used by both the test runner and the fake API server. It includes the main logic (`main.go`, `scenario.go`) and specific scenario handlers for DNS (`scenario_dns.go`) and Gateway (`scenario_gateway.go`) interactions. The library depends on Kubernetes API types and controller-runtime clients to interact with the in-cluster state.

The `aquaducte2e` binary is built as a static, pure Go executable to ensure compatibility with the airgapped Firecracker microVM environment, which lacks libc. This single binary serves dual purposes: it acts as the test driver that orchestrates scenarios and as the "fake" Aquaduct.dev API server running inside the VM. The `fake` image target packages this binary for use in the test environment.

The `operator_manifest` target generates the Helm chart manifests for the weft-operator, allowing the e2e test to install the real operator into the k3s cluster. Configuration is provided via `e2e-values.yaml`, specifically pointing the operator's authentication endpoint at the local fake server.

Finally, `aquaduct_test` ties these components together using the `weft_e2e_test` rule. It injects the generated operator manifest and the fake server image into the test environment. The test executes a comprehensive sequence of scenarios, including TaaS lifecycle management (secret missing/synced/token caching), DNS record registration and resolution (including mismatch handling and foreign domain detection), and Gateway listener synchronization. These tests verify that the operator correctly handles edge cases such as API errors, token revocation, and deletion ordering.