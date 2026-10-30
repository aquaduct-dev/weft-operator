<!-- docdaemon ts:2026-10-30T02:59:29Z hash:1cf2ad23be0c9a6f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes namespaces required to isolate the Weft operator control plane and its associated e2e test workloads.

The document creates two distinct `Namespace` resources to enforce separation of concerns during end-to-end testing:

*   **`weft-system`**: The dedicated namespace for the operator itself. This ensures the operator's lifecycle, logs, and resources are isolated from user workloads.
*   **`e2e-app`**: The namespace used for deploying test applications and validating operator behavior against specific workloads.

By explicitly defining these namespaces in a single manifest, the e2e suite guarantees that the cluster state is initialized with the correct scoping before any Helm charts or CRDs are applied. This structure supports tests that verify operator readiness, crash-looping behaviors (such as missing Gateway API CRDs), and successful reconciliation within their respective boundaries.