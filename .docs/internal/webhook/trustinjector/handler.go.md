<!-- docdaemon ts:2026-10-30T02:59:59Z hash:c8fb7167eb100e60 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the mutating admission webhook handler that injects WeftLocalDNSReflector CA trust bundles into new pods.

The `Handler` struct processes admission requests to ensure pods trust the local DNS reflector's certificates. It decodes incoming pod specs and checks for opt-out annotations or labels. If injection is required, it builds the current trust bundle and ensures a corresponding ConfigMap exists in the target namespace.

Key behaviors include:
- **Failure Policy**: The webhook is registered with `failurePolicy: Ignore`. If the operator is unavailable or errors occur (e.g., missing bundle, decode failure), the pod is admitted unmodified. This prevents cluster-wide outages if the injector fails.
- **ConfigMap Management**: For non-dry-run requests, it creates the trust bundle ConfigMap if it doesn't exist. It explicitly rejects injection if a foreign ConfigMap already occupies the expected name (`trust.ConfigMapName`) to avoid conflicts, admitting the pod without trust instead.
- **Dry Run Support**: During dry runs, it validates the ConfigMap state without creating resources, ensuring side-effect-free validation.
- **Injection Logic**: It modifies the pod spec to mount the trust bundle volume and configure container environments, returning a JSON patch to the API server.

The handler relies on `trust.Source` for bundle construction and `admission.Decoder` for request parsing. It fits alongside `setup.go`, which manages the webhook serving certificate and `MutatingWebhookConfiguration`.