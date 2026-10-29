<!-- docdaemon ts:2026-10-29T20:35:49Z hash:8cb57f5018455fd0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines a Kubernetes Secret containing self-signed TLS certificates required for the VPA admission webhook.

The template generates an `Opaque` secret named `vpa-tls-certs` in the release namespace. It uses Helm's built-in `genCA` and `genSignedCert` functions to create a CA and a server certificate, ensuring the keys match the filenames expected by the admission controller (`caCert.pem`, `serverCert.pem`, `serverKey.pem`). This approach replaces out-of-band script execution with in-chart generation.

The resource is conditionally rendered only when the `installVpaController` helper returns true, indicating that the VPA component is being bootstrapped. The `lookup` function is utilized to retrieve existing secrets during upgrades, preventing CA rotation issues that could break the self-registered `MutatingWebhookConfiguration`.