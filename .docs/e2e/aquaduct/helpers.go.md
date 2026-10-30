<!-- docdaemon ts:2026-10-30T02:59:25Z hash:104c70df5f512a96 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides helper utilities for end-to-end tests, managing a fake API control plane and Kubernetes resource interactions.

### Fake API Control
The `scenario` struct exposes methods to interact with an in-cluster fake API via its ClusterIP. `fakeDo` handles generic HTTP requests to `/_fake` endpoints, while specific helpers like `requests`, `setBastions`, and `setLookup` manage test state. The `waitRequests` function polls the request log until a condition is met, enabling assertions on external calls.

### Kubernetes Resource Helpers
Functions prefixed with `touch` (e.g., `touchTaaS`, `touchRecord`) force controller resyncs by patching annotations. `getTaaS` and `getRecord` retrieve current resource states. `taasAvailable` and `waitRecord` are polling wrappers that wait for specific status conditions or checks on `AquaductTaaS` and `DNSRecord` resources, using `e2ekit.WaitFor` to handle timeouts gracefully.