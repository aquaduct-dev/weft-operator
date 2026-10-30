<!-- docdaemon ts:2026-10-30T02:59:57Z hash:a07bc870d99637c4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains integration tests verifying that the WeftTunnel controller correctly updates its status conditions based on the availability of underlying Kubernetes Deployments.

The test suite (`Describe("WeftTunnel Controller Status")`) validates the reconciliation loop's ability to reflect resource health in the Custom Resource (CR) status. It follows a standard Ginkgo/Gomega pattern:

1.  **Setup**: Creates a `WeftServer` and a `WeftTunnel` CR, then initializes the reconciler.
2.  **Initial Reconcile**: Triggers the first reconcile cycle. The test notes that the current implementation may incorrectly report `Available=True` immediately, even if the Deployment has zero replicas. This highlights a known bug where status is set unconditionally on success rather than checking actual replica counts.
3.  **Verification of Ownership**: Ensures the generated Deployment has the correct owner reference (`WeftTunnel`) so that subsequent updates trigger re-queues.
4.  **Simulate Readiness**: Manually patches the Deployment’s status to indicate it is ready (`ReadyReplicas=1`).
5.  **Final Reconcile**: Runs the reconciler again and asserts that the `WeftTunnel` status condition `Available` is now correctly set to `True`.

This test serves as a regression check for status propagation logic, ensuring that tunnel availability accurately mirrors the state of its backing infrastructure.