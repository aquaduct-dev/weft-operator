<!-- docdaemon ts:2026-10-30T02:59:40Z hash:ffa4abd2ca9847b4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides utilities to retrieve Prometheus metrics from the `weft-operator` pod by entering its network namespace.

### Purpose
The `e2e/trust/netns.go` package enables end-to-end tests to verify operator behavior by reading `/metrics` directly from within the operator's container. Because the metrics endpoint is bound to `127.0.0.1:8080` inside the pod, this code uses Linux system calls to switch the test runner's OS thread into the pod's network namespace (`netns`) before dialing localhost.

### Key Functions

*   **`operatorMetrics`**: Retrieves the raw text of the operator's metrics endpoint. It locates the operator container's PID, opens the corresponding `/proc/<pid>/ns/net` file descriptor, and uses `setns(2)` to join that network namespace on a locked OS thread. It then performs an HTTP GET request to `127.0.0.1:8080/metrics`. The thread lock ensures the runtime does not migrate the goroutine to another OS thread after the namespace change, which would break the connection context.
*   **`webhookRequests`**: Parses the metrics body returned by `operatorMetrics` to calculate the total number of requests made to the trust injection webhook (`/inject-trust`). It sums the `controller_runtime_webhook_requests_total` counter for all response codes associated with that specific webhook path.

### Context
Recent changes to the e2e suite introduced checks for the trust webhook's MutatingWebhookConfiguration and CA bundle handling. This file supports those tests by providing a reliable way to observe webhook invocation counts without relying on external log aggregation or port-forwarding, ensuring tests run accurately against the live cluster state.