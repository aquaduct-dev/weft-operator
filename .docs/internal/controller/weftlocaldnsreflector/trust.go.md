<!-- docdaemon ts:2026-10-29T20:35:59Z hash:f7dad522d4fed8de model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file manages the distribution of the shared trust bundle across namespaces and determines if running pods are ready to use the reflector.

### Purpose
The `trust.go` file implements the client-side trust logic for the WeftLocalDNSReflector. It ensures that every namespace contains a ConfigMap with the current CA bundle, allowing the injection webhook to mount it. Additionally, it computes a "trust gate" by identifying pods that do not yet trust the reflector's CA, ensuring that enabling the reflector does not break existing workloads.

### Key Functions

*   **`publishTrustBundle`**: Iterates over all active namespaces and creates or updates a ConfigMap containing the system roots and reflector CAs. It skips terminating namespaces and ignores unrelated ConfigMaps that happen to share the expected name.
*   **`untrustedPods`**: Queries the API server directly (bypassing the cache) for running pods. It counts and samples up to 20 pod names that lack the required CA annotation, providing visibility into which pods are not yet trusting the reflector.
*   **`exemptFromTrust`**: Determines if a pod should be excluded from trust checks. This mirrors the webhook's exclusion logic, skipping pods in excluded namespaces, those explicitly opted out via labels/annotations, and static mirror pods.

### Integration
This code works in tandem with `internal/trust` for bundle construction and the admission webhook for injection. The `untrustedPods` result is used to enforce the trust gate, preventing DNS overrides until all running pods have been updated to trust the new CA.