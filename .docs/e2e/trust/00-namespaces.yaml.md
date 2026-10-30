<!-- docdaemon ts:2026-10-30T02:59:35Z hash:4c03e7ec4d9db885 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes namespaces and initial ConfigMap resources required to set up the environment for trust webhook end-to-end tests.

It creates three distinct namespaces: `weft-system` for the operator, `e2e-trust` for workloads injected by the webhook, and `e2e-foreign` for testing isolation scenarios. The `e2e-foreign` namespace includes a pre-existing `weft-trust-bundle` ConfigMap owned by an external entity (`someone-else`). This setup allows tests to verify that the reflector correctly handles cases where a trust bundle already exists in a foreign namespace before any reflection occurs, ensuring proper boundary enforcement and injection logic.