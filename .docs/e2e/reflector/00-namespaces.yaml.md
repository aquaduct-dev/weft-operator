<!-- docdaemon ts:2026-10-29T20:35:53Z hash:6ef3ee469ac03625 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes namespaces required to isolate the reflector operator, its backend services, and client workloads during end-to-end testing.

The document creates three distinct `Namespace` resources to enforce strict separation of concerns within the test environment:

*   **`weft-system`**: The namespace where the reflector operator is installed and runs. This isolates the control plane components from test artifacts.
*   **`e2e-backend`**: The namespace hosting the backend services that the reflector publishes. This allows tests to verify traffic routing and service discovery independently of the operator.
*   **`e2e-client`**: The namespace for client workloads that consume the reflected services. This ensures that client-side validation (such as DNS resolution checks) occurs in a clean, isolated context.

These namespaces are prerequisites for the reflector e2e tests, which validate behaviors such as in-cluster traffic retention and DNS trust gates. By pre-defining these scopes, the test suite avoids namespace pollution and ensures deterministic state before deploying the operator or running integration checks.