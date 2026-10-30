<!-- docdaemon ts:2026-10-30T02:59:39Z hash:532456af500ddd9c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the `lifecycleScenario` E2E test suite that validates a reflector’s behavior through certificate reissuance, proxy failures, state restoration, and feature toggles.

### Purpose
The `lifecycleSteps` function orchestrates a sequence of seven steps to verify the full lifecycle of a `WeftLocalDNSReflector`. It ensures the operator correctly manages serving certificates, proxy availability, node agent state (`/etc/hosts`, trust store), and DNS overrides.

### Key Components
- **`lifecycleScenario`**: Holds test context, including the service IP and shared extras.
- **`ready`**: Deploys the backend, creates a tunnel for `hostA`, and verifies initial readiness and node agent entries.
- **`reissue`**: Patches the reflector to add `hostB`, verifying that the serving certificate is reissued with new SANs, the proxy rolls, and unrouted hostnames are tracked until their tunnels exist.
- **`proxyDown`**: Simulates proxy unavailability by cordoning nodes and deleting pods. Verifies that DNS is withdrawn and the node agent removes `/etc/hosts` entries due to TLS failure, then restores them when the proxy returns.
- **`agentReasserts`**: Deletes node state files manually to confirm the agent automatically re-applies `/etc/hosts`, CA bundles, and `certs.d` entries.
- **`clusterDNSDisabled`**: Disables cluster DNS to ensure CoreDNS overrides are removed while node-level entries persist.
- **`nodeTrustDisabled`**: Disables node trust to verify the complete removal of the node agent and all associated node state.

### Helpers
- **`waitStatusFor`** and **`waitReady`**: Poll the reflector status until conditions (e.g., `Ready=True`, specific annotations) are met.
- **`waitNode`**: Waits for specific lines in `/etc/hosts`, CA bundle presence, and `certs.d` files on the node.
- **`setUnschedulable`**: Marks all nodes as unschedulable to force pod eviction during the `proxyDown` test.