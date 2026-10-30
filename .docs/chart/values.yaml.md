<!-- docdaemon ts:2026-10-30T02:59:21Z hash:f9ad3986e34296ca model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the default configuration values for the weft-operator Helm chart, controlling deployment resources, autoscaling behavior, and cluster-wide DNS reflection.

### Image Configuration
- `image.url`: The operator container image, sourced from the build system.
- `weftImage`: The base image for Weft components (tunnel clients/servers, probes).
- `imagePullSecrets`: Credentials for private registry access.

### Operator Resources & Autoscaling
The operator runs with static CPU limits but dynamic memory management via Vertical Pod Autoscaler (VPA):
- `resources`: Sets a 250Mi memory request and 1Gi limit to prevent OOM kills while allowing burst capacity.
- `verticalPodAutoscaler`:
  - `enabled: true` activates VPA for the operator Deployment.
  - `updateMode: "Off"` ensures VPA only recommends resources without mutating the running pod, preserving predictable resource allocation.
  - `installController: auto` installs the VPA controller stack if not present on the cluster.
  - Bounds (`minAllowedMemory`, `maxAllowedMemory`) constrain recommendations between 250Mi and 1Gi.

### Cluster-Wide DNS Reflection
- `defaultReflector`: Creates a `WeftLocalDNSReflector` named `cluster-default` by default. This keeps traffic to Weft-published hostnames within the cluster, avoiding hairpinning through the bastion. Set `enabled: false` to disable this automatic reflector.

### Gateway Integration
- `gatewayClass`: Manages the `GatewayClass` resource used by Ingress/Gateway objects to route traffic through the operator. Users can provide custom parameters via `parametersRef`.

### Security Context
- Enforces non-root execution (`runAsNonRoot: true`, `runAsUser: 1000`).
- Drops all capabilities and sets a read-only root filesystem to minimize attack surface.