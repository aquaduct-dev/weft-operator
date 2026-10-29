<!-- docdaemon ts:2026-10-29T20:36:19Z hash:a3cb6d6c8f05dbb0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file bootstraps the trust-injection webhook by managing its TLS certificate lifecycle and registering the MutatingWebhookConfiguration with the Kubernetes API server.

### Purpose
The `trustinjector` package ensures the API server can securely communicate with the operator’s webhook service. It handles two critical concerns: maintaining a valid, long-lived serving certificate that survives operator restarts, and configuring the admission controller to intercept pod creation events.

### Key Components

**`Config`**
Defines the operational parameters for the webhook, including the target Service namespace/name, the local directory for writing TLS files (`CertDir`), and a list of `ExcludedNamespaces`. These excluded namespaces are bypassed by both the webhook injection logic and the reflector trust gate.

**`EnsureServingCert`**
Reconciles a Secret containing the CA and serving certificates.
- If the CA is missing or invalid, it generates a new one.
- If the leaf certificate needs renewal (based on expiration or DNS name changes), it issues a new leaf signed by the CA.
- Writes `tls.crt` and `tls.key` to `cfg.CertDir` so the manager’s cert watcher can pick them up.
- Returns the CA PEM bundle for use in the webhook configuration.

**`EnsureConfiguration`**
Creates or updates the `MutatingWebhookConfiguration` named `weft-operator-trust-injector`.
- Points the API server to the operator’s webhook Service via `ServiceReference`.
- Injects the CA bundle from `EnsureServingCert` into `CABundle`.
- Targets `pods` on `Create` operations.
- Excludes namespaces listed in `cfg.ExcludedNamespaces` using a label selector on `kubernetes.io/metadata.name`.
- Respects opt-outs via the `trust.aquaduct.dev/opt-out` label.

**`Setup` & `Renewer`**
- `Setup` runs `EnsureServingCert` followed by `EnsureConfiguration`, intended for initial startup.
- `Renewer` is a `manager.Runnable` that executes `Setup` periodically (defaulting to every 24 hours) to ensure the serving certificate is renewed before expiration, preventing brief periods where the webhook might be untrusted after an operator restart.