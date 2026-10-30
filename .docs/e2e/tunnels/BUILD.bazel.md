<!-- docdaemon ts:2026-10-30T02:59:40Z hash:759a8c12b73429eb model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the end-to-end tests that validate weft servers, tunnels, and the Gateway controller within a simulated k3s environment.

It compiles `tunnels_lib` from Go sources into a static binary (`tunnelse2e`) to ensure compatibility with the microVM rootfs lacking libc. This same binary serves as both the test driver and the in-cluster echo backend/ACME stand-in.

The `helm_template` target renders the real operator chart into `operator_manifest`, configuring it with specific e2e values (GatewayClass on, reflector off).

Two `weft_e2e_test` rules orchestrate the scenarios:
- `tunnels_test`: Validates full traffic flow (HTTP/TCP/UDP), Gateway integration, tunnel lifecycle, and server status metrics.
- `prober_test`: Validates the NodeProber logic in isolation, ensuring correct server creation and cleanup based on node reachability.