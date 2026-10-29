<!-- docdaemon ts:2026-10-29T20:36:19Z hash:8b581e52d1394c34 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains integration tests for the `WeftTunnel` controller's status update logic, verifying that tunnel availability conditions reflect the underlying Deployment's readiness.

The test suite uses Ginkgo and Gomega to validate the reconciliation loop of `wefttunnel.WeftTunnelReconciler`. It creates a `WeftServer` and a `WeftTunnel` resource, then triggers reconciliation to ensure the controller correctly manages the lifecycle of the associated Kubernetes Deployment.

Key behaviors verified include:
- **Initial State**: The test asserts that the tunnel's `Available` condition is not immediately set to `True` upon creation, acknowledging that the Deployment requires time to become ready.
- **Readiness Propagation**: After manually updating the Deployment's status to indicate one ready replica, the test re-reconciles the tunnel and verifies that the `Available` condition updates to `True`.

The structure reflects recent refactoring efforts to use a resource CRUD framework, ensuring the reconciler interacts with standard Kubernetes client interfaces (`k8sClient`) rather than direct API calls. This approach isolates the controller logic from external dependencies while validating end-to-end status synchronization.