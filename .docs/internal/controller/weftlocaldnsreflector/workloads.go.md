<!-- docdaemon ts:2026-10-29T20:36:00Z hash:8911f2bf87fd6138 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file reconciles the Kubernetes resources (Deployment, Service, DaemonSet) required for the WeftLocalDNSReflector to route traffic and manage node trust.

### Purpose
It manages three core components:
1.  **Proxy Deployment**: Terminates TLS and routes traffic based on configured routes. Scales to zero if no routes exist.
2.  **ClusterIP Service**: Exposes the proxy internally.
3.  **Node Agent DaemonSet**: Runs on every node to update `/etc/hosts` and configure containerd trust, ensuring local resolution of public hostnames.

### Key Functions
*   `ensureProxy`: Reconciles the proxy Deployment. It handles replica scaling (0 or 1+), TLS volume mounting, and readiness probes.
*   `ensureService`: Creates a ClusterIP Service mapping ports 80/443 to the proxy's target ports. Returns the allocated ClusterIP.
*   `ensureNodeAgent`: Manages the DaemonSet. Deletes it if node trust is disabled or no service IP exists. Otherwise, configures host network access, security contexts, and volume mounts for CA bundles and hosts files.
*   `pullPolicy`: Determines image pull policy (`Always` for untagged/latest, `IfNotPresent` otherwise) to support air-gapped environments.
*   `mergeServicePorts` / `setVolume`: Helper functions that preserve API server defaults during updates to ensure idempotency with `CreateOrUpdate`.

### Design Notes
*   **Idempotency**: Mutations only touch owned fields, allowing the API server's defaults to persist and preventing unnecessary updates.
*   **Security**: The node agent runs as root with minimal capabilities (`DAC_OVERRIDE`) to edit host files securely.
*   **Network**: The node agent uses `HostNetwork` to ensure TLS health checks match the path containerd takes to the ClusterIP.