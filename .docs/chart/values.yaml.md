<!-- docdaemon ts:2026-10-29T20:35:54Z hash:a0608c17acb8aa2f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the default configuration values for the Weft Operator Helm chart, controlling deployment resources, autoscaling behavior, and cluster-wide DNS reflection settings.

### Image Configuration
- `image.url`: The operator container image, sourced from the build pipeline.
- `image.pullPolicy`: Set to `Always` to ensure the latest image is used.
- `weftImage`: The version of the `weft` binary used by sidecar reflectors.

### Resource Management & Autoscaling
The operator runs with static CPU limits but uses a Vertical Pod Autoscaler (VPA) for memory management.
- `resources`: Static requests/limits define the baseline. Memory request is 250Mi, limit is 1Gi. This prevents OOMKills during informer-cache spikes while keeping baseline reservation low.
- `verticalPodAutoscaler`:
  - `enabled: true`: Activates VPA for the operator Deployment.
  - `updateMode: "Off"`: VPA only recommends resources; it does not mutate the running pod. This ensures predictable runtime behavior since the shared cluster updater cannot evict single-replica pods.
  - `installController: auto`: Installs the VPA controller stack only if missing from the cluster.
  - `minReplicas: 1`: Overrides the upstream default of 2 to allow the updater to act on the single operator replica if mode changes.

### Cluster-Wide DNS Reflection
- `defaultReflector`: Creates a `WeftLocalDNSReflector` named `cluster-default` by default.
  - `hostnames: []`: Reflects all hostnames served by WeftTunnels (`allHostnames`).
  - This keeps in-cluster traffic local, avoiding hairpinning through the bastion/WireGuard tunnel.
  - Setting `enabled: false` causes the operator to delete this reflector.

### Gateway Integration
- `gatewayClass`: Manages a `GatewayClass` resource named `weft-gateway-class`.
  - Users reference this class in their `Gateway` objects to route traffic through the operator.
  - `parametersRef` can optionally point to a `WeftGateway` CR for per-class configuration.