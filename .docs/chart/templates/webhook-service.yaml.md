<!-- docdaemon ts:2026-10-29T20:35:51Z hash:ce7ad606b4740614 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes Service that exposes the trust injection webhook endpoint for the API server.

The `Service` resource provides a stable network entry point for the MutatingWebhookConfiguration managed by the operator. It uses a `ClusterIP` type to ensure the webhook is accessible only within the cluster network, which is critical for secure internal communication between the API server and the operator's controller.

Key configuration details include:
- **Name**: Dynamically generated using the release fullname with a `-webhook` suffix.
- **Selector**: Targets pods matching the standard operator labels, ensuring traffic reaches the correct webhook handler instance.
- **Port Configuration**: Exposes port 443 (standard HTTPS) and forwards it to the container port named `webhook`. This aligns with the TLS requirements of Kubernetes webhooks, where the API server expects a secure connection.

The service works in tandem with the operator-managed MutatingWebhookConfiguration. Because the operator controls both the certificate serving the webhook and the CA bundle injected into the webhook configuration, the `caBundle` always matches the certificate presented by this service, preventing authentication errors during trust injection.