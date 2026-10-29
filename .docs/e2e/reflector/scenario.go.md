<!-- docdaemon ts:2026-10-29T20:36:03Z hash:0c11dd191b83ce1d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the end-to-end test scenario for the WeftLocalDNSReflector, validating DNS resolution, TLS trust injection, and in-cluster traffic routing.

The `scenario` struct orchestrates a sequence of `step`s executed via `run`, which halts on failure and dumps diagnostics. Key steps include:

*   **Setup**: `installOperator` verifies operator readiness and webhook registration; `deployBackend` creates a local registry.
*   **Trust Gate**: `createReflectorAndCheckGate` ensures DNS is withheld until pods are injected with the CA certificate.
*   **Validation**: `checkPodPath` and `checkNodePath` verify that pods and containerd pull images through the reflector proxy. `checkNodeHostPath` confirms node-level processes resolve the hostname and validate TLS using the system trust store.
*   **Teardown**: `checkTeardown` ensures all DNS entries, host mappings, and trust store modifications are cleaned up upon reflector deletion.

Helper functions like `dumpDiagnostics` provide context for debugging failed steps by logging pod states, events, and configuration files.