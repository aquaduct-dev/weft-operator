<!-- docdaemon ts:2026-10-30T02:59:37Z hash:beb16102d175e9a4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file serves as the entry point for the guest-side e2e test binary that validates the trust-injection webhook behavior within a firecracker microVM.

The `main` function implements two distinct execution modes to support different testing phases. If the first argument is `"sleep"`, the process blocks indefinitely, allowing the container image to serve as a long-running pod with a standard system trust store. Otherwise, it delegates to `e2ekit.Main`, executing the `webhook` scenario defined in `webhookSteps`. This design enables the binary to act as both a static reference environment and an active driver for the API server interactions required by the webhook test suite.