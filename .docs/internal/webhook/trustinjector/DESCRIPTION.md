<!-- docdaemon ts:2026-10-30T03:00:09Z hash:087f6ecfb0024318 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `trustinjector` package implements a mutating admission webhook that injects WeftLocalDNSReflector CA trust bundles into pods.

## Overview
This directory provides the core logic for ensuring cluster pods trust the local DNS reflector’s certificates, enabling secure in-cluster traffic routing. It manages the lifecycle of serving TLS certificates and registers the MutatingWebhookConfiguration with the Kubernetes API server to intercept pod creation events. The system is designed to be resilient, ignoring failures to prevent cluster-wide outages while respecting explicit opt-out mechanisms.

## Deep Dive
### Webhook Handler
The `Handler` processes admission requests by decoding pod specs and checking for opt-out annotations or labels. If injection is required, it constructs the current trust bundle using `trust.Source` and ensures a corresponding ConfigMap exists in the target namespace. To avoid conflicts, it rejects injection if a foreign ConfigMap already occupies the expected name. The handler modifies the pod spec to mount the trust bundle volume and configure container environments, returning a JSON patch. The webhook uses a `failurePolicy: Ignore` strategy, admitting unmodified pods if errors occur, which prevents operator unavailability from blocking cluster operations. Dry runs are supported to validate state without creating resources.

### Certificate and Configuration Management
The `setup.go` file bootstraps the webhook by managing TLS certificate lifecycles and API registration. `EnsureServingCert` reconciles a Secret containing CA and serving certificates, generating new ones if expired or invalid, and writes them to disk for the manager’s cert watcher. `EnsureConfiguration` creates or updates the `MutatingWebhookConfiguration`, pointing the API server to the operator’s webhook Service and injecting the CA bundle. It targets `pods` on `Create` operations and excludes namespaces via label selectors. A `Renewer` component runs periodically to ensure certificates are renewed before expiration, maintaining stable CA identity across operator restarts.

## Key Interfaces
- `Handler.Handle`: Processes admission requests to inject trust bundles.
- `Config`: Defines operational parameters including service reference and excluded namespaces.
- `EnsureServingCert`: Reconciles TLS certificates and returns the CA PEM bundle.
- `EnsureConfiguration`: Creates or updates the MutatingWebhookConfiguration.
- `Setup`: Runs initial certificate and configuration setup.
- `Renewer`: A manager.Runnable for periodic certificate renewal.

## Dependencies
- Internal PKI and trust packages for certificate management.
- Kubernetes API types and controller-runtime utilities.
- Fake Kubernetes client for testing.

## Subcomponents
- `handler.go`: Webhook mutation logic.
- `setup.go`: Certificate and configuration bootstrap.
- `trustinjector_test.go`: Unit tests for handler and setup.
- `BUILD.bazel`: Bazel build targets.