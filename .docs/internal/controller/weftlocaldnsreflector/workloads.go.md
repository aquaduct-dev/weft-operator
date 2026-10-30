<!-- docdaemon ts:2026-10-30T02:59:54Z hash:58c4786c35792cc3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file reconciles the Kubernetes resources (Deployment, Service, DaemonSet) required to reflect public DNS traffic into the cluster.

### Purpose
It manages the lifecycle of the `weft proxy` and node agent components for a `WeftLocalDNSReflector`. The code ensures that internal pods can reach public hostnames via in-cluster services, avoiding hairpinning through external bastions. It uses idempotent mutation patterns (`CreateOrUpdate`) to preserve API server defaults and minimize unnecessary updates.

### Key Functions
- **`ensureProxy`**: Reconciles the `weft proxy` Deployment. Scales to zero if no routes are configured. Manages TLS volume mounts and readiness probes based on route schemes.
- **`ensureService`**: Creates a ClusterIP Service exposing the proxy’s HTTP/HTTPS ports. Returns the allocated ClusterIP for downstream use.
- **`ensureNodeAgent`**: Reconciles a DaemonSet running on nodes to update `/etc/hosts` and containerd trust stores. Deletes itself if node trust is disabled or no service IP is available. Requires the operator image to be set.
- **`deploymentAvailable`**: Checks if the proxy deployment has ready replicas.

### Implementation Details
- **Idempotency**: Helper functions like `setVolume`, `mergeServicePorts`, and `containerByName` modify only owned fields, allowing safe retries without overwriting server-side defaults.
- **Security**: The node agent runs as root with minimal capabilities (`DAC_OVERRIDE`) to edit host files. The proxy automounts service account tokens as false.
- **Network**: The node agent uses `HostNetwork` to ensure TLS health checks match the path containerd takes to the ClusterIP.