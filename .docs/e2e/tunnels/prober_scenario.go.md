<!-- docdaemon ts:2026-10-30T02:59:45Z hash:71fe100d57c37781 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the "prober" end-to-end test scenario, validating the NodeProber's success path for auto-creating WeftServers.

The `proberScenario` struct orchestrates a sequence of steps to verify that the operator correctly probes a node, discovers its public IP, and creates a corresponding server resource. It runs in an isolated VM environment where the node is configured with a documentation IPv4 (`198.51.100.10`) on the loopback interface and a stand-in HTTP server responding with 404s to ACME challenge requests.

Key functions include:
*   **`nodeSetup`**: Configures the test node's network interface and deploys an `acme-standin` deployment to simulate autocert behavior.
*   **`install`**: Deploys the operator using the provided chart manifest and CRDs.
*   **`jobSucceeds`**: Waits for the startup probe Job to complete successfully, ensuring the initial discovery phase passed.
*   **`serverCreated`**: Verifies that a `WeftServer` named `node-server0` was created with correct labels and a connection string matching the probed IP.
*   **`serverAvailable`**: Confirms the auto-created server pod becomes available and reports `Available=True`.
*   **`restart`**: Tests idempotency by deleting operator pods, verifying that existing servers are preserved, no new probe jobs are spawned, and orphaned servers (like `node-ghost`) are cleaned up.