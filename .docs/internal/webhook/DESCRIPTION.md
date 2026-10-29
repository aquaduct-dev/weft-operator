<!-- docdaemon ts:2026-10-29T20:38:53Z hash:090f9147e89a14c0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The trust injector webhook manages PKI certificates and handles admission requests to inject in-cluster DNS resolution capabilities into pods.

## Overview
This directory implements a mutating admission webhook that intercepts pod creation events to inject WeftLocalDNSReflector CA certificates. It ensures that intra-cluster traffic, particularly to public hostnames, is routed through WireGuard tunnels rather than hairpinning through bastion hosts, thereby reducing latency for bulk operations like image registry pulls. The package also manages the lifecycle of TLS serving certificates and registers the webhook configuration with the Kubernetes API server.

## Deep Dive
### Webhook Handler
The `Handler` struct processes admission reviews for Pod resources. It checks for opt-out annotations via `trust.OptedOut` and builds the current trust bundle using `trust.Source.Build`. If the bundle is non-empty, it ensures the namespace-specific ConfigMap containing the trust bundle exists by calling `CreateConfigMapIfMissing`, skipping this step during dry-run requests to avoid side effects. The handler then injects the CA bundle into the pod's volumes and environment variables using `trust.InjectPod`. Errors result in an "Allowed" response with an unmodified pod, ensuring the webhook failure policy of `Ignore` prevents blocking pod creation.

### Certificate Lifecycle Management
The `EnsureServingCert` function reconciles a Secret containing the CA and serving certificates. It generates a new CA if missing or invalid, and issues a new leaf certificate signed by the CA if renewal is needed based on expiration or DNS name changes. These certificates are written to `cfg.CertDir` so the manager’s cert watcher can pick them up. A `Renewer` component runs `Setup` periodically to ensure the serving certificate is renewed before expiration, preventing brief periods where the webhook might be untrusted after an operator restart.

### Webhook Configuration
The `EnsureConfiguration` function creates or updates the `MutatingWebhookConfiguration` named `weft-operator-trust-injector`. It points the API server to the operator’s webhook Service via `ServiceReference` and injects the CA bundle from `EnsureServingCert`. The configuration targets `pods` on `Create` operations and excludes namespaces listed in `cfg.ExcludedNamespaces` using a label selector on `kubernetes.io/metadata.name`. It also respects opt-outs via the `trust.aquaduct.dev/opt-out` label.

## Key Interfaces
- `Handler.Handle`: Processes admission requests for Pod creation.
- `Config`: Defines operational parameters including Service namespace/name and excluded namespaces.
- `EnsureServingCert`: Reconciles TLS certificates and writes them to disk.
- `EnsureConfiguration`: Creates or updates the MutatingWebhookConfiguration.
- `Setup`: Runs initial certificate and configuration setup.
- `Renewer`: Runnable that executes Setup periodically to renew certificates.

## Dependencies
- Internal PKI and trust packages for certificate management.
- Kubernetes API types for handling admission reviews.
- Controller-runtime fake client for testing cluster interactions.

## Subcomponents
- `handler.go`: Implements the mutating admission webhook handler.
- `setup.go`: Bootstraps the webhook and manages TLS certificate lifecycle.
- `trustinjector_test.go`: Unit tests for handler and setup logic.
- `BUILD.bazel`: Bazel build targets for the package.