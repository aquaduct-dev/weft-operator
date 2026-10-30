<!-- docdaemon ts:2026-10-30T02:59:41Z hash:4ca04671b722a903 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the end-to-end test scenario for the trust-injection webhook, verifying pod mutation logic against a live API server.

The `webhookScenario` struct orchestrates a sequence of steps defined in `webhookSteps`, starting with operator installation and configuration validation. It verifies that the `MutatingWebhookConfiguration` correctly targets pod creation while excluding system namespaces and respecting opt-out labels. The tests confirm that pods are only injected with the CA bundle after a `WeftLocalDNSReflector` becomes ready, ensuring the injected volume is read-only (0444) and mounted at standard system paths.

Key behaviors validated include:
- **Injection Logic**: Pods receive an annotation and volume mounts only when a reflector exists.
- **Exemptions**: Namespaces like `kube-system` are skipped, and label-based opt-outs prevent handler invocation.
- **Resilience**: The webhook handles dry runs without side effects, ignores failures when the operator is down (relying on a trust gate), and maintains stable serving certificates across restarts.
- **Namespace Isolation**: New namespaces trigger on-demand bundle publishing, and unrelated ConfigMaps named similarly to the trust bundle are ignored.

Helper functions like `uninjected` and `bundleVolume` enforce strict checks on pod specs, ensuring no residual or incorrect modifications occur during edge cases such as covered mounts or foreign resources.