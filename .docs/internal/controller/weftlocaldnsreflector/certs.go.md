<!-- docdaemon ts:2026-10-29T20:36:14Z hash:4d338d9d15a5fd7e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file manages the lifecycle of TLS certificates for the WeftLocalDNSReflector by ensuring CA and serving Secrets exist and are valid.

### Purpose
The `ensureCertificates` function guarantees that two Kubernetes Secrets are present in the reflector’s namespace: one for the self-signed Certificate Authority (CA) and one for the serving certificate. It handles creation, renewal, and ownership management without exposing key material outside the cluster.

### Key Components

*   **`issuedCertificates`**: A struct returned to the reconciler containing the CA PEM data, the serving certificate fingerprint, and its expiration time. This allows downstream logic to verify trust chains and handle expiry.
*   **CA Secret Management**: If the CA Secret does not exist, it is generated using `pki.NewCA`. The secret is labeled, typed as `kubernetes.io/tls`, and set as an owner reference to the `WeftLocalDNSReflector` resource.
*   **Serving Certificate Renewal**: The function uses `controllerutil.CreateOrUpdate` to manage the serving Secret. It checks if renewal is needed via `pki.RenewalReason`, which triggers regeneration if:
    *   The SANs drift from the requested hostnames.
    *   The certificate no longer chains to the current CA.
    *   The certificate is close to expiry.
*   **Security Constraints**: Key material remains strictly within the Secrets. No keys are logged or exposed in the `issuedCertificates` return value, only the public CA cert and the leaf certificate's fingerprint.