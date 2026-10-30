<!-- docdaemon ts:2026-10-30T02:59:33Z hash:169c268675f824c3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides Helm chart values to configure the `crds_test` environment for end-to-end testing of the operator's CRD preflight logic.

It configures a minimal runtime by disabling features not relevant to the specific test scenario, such as Vertical Pod Autoscaler, GatewayClass creation, and the default reflector. Crucially, it sets image pull policies to `Never` to ensure that preloaded local images are used without attempting network pulls, which is essential for isolated CI environments.

The configuration supports a test flow where the operator boots without Gateway API CRDs present. The setup verifies that the operator detects the missing CRDs, exits with a preflight error, and crash-loops. Subsequent steps in the test apply the standard channel CRDs, after which the operator must become Ready, log ignored routes (TCPRoute/TLSRoute/UDPRoute), and successfully convert an HTTP Gateway into a WeftTunnel. This file ensures the initial state reflects the "no CRDs" condition required to validate this recovery path.