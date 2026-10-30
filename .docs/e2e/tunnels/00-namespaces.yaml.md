<!-- docdaemon ts:2026-10-30T02:59:40Z hash:f82261ce7f7ecf5b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes namespaces required to isolate resources for the tunnels and prober end-to-end tests.

It establishes three distinct namespaces to manage the test environment's lifecycle and resource isolation:

*   **`weft-system`**: The control plane namespace where the WeftServer operator and its primary pods reside. This isolates the core infrastructure from test-specific workloads.
*   **`e2e-gw`**: The data plane namespace for the Gateway controller and backend services. It hosts the standard WeftServers (typically on ports 9092 and 9093) and validates routing, fan-out, and header filtering behaviors.
*   **`e2e-alt`**: An alternative namespace used to test non-default configurations. It contains a WeftServer running on a custom port and manually created WeftTunnels, ensuring the system handles edge cases and explicit tunnel definitions correctly.

These namespaces are referenced by the `tunnels_test` suite in `e2e/tunnels`, which exercises HTTP, TCP, and UDP traffic flows through the operator-managed pods.