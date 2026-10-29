<!-- docdaemon ts:2026-10-29T20:36:18Z hash:e56933487b7727e7 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file verifies that the WeftTunnel reconciler stops updating the Deployment resource once it has converged to the desired state.

The test suite ensures idempotency by wrapping the Kubernetes client in a `countingClient`. This wrapper increments a counter every time an `Update` is called on a `Deployment`, allowing the test to assert that no writes occur after initial creation.

The test creates a `WeftServer` and a `WeftTunnel`, then triggers reconciliation. It explicitly checks that `TerminationMessagePath` is populated, as API server defaults are critical for preventing infinite update loops. If the reconciler overwrites these defaults with empty values, the API server re-applies them, causing a perpetual diff. The test confirms that subsequent reconcile calls do not trigger further updates, proving the controller respects the current state and avoids starving the operator loop with redundant writes.