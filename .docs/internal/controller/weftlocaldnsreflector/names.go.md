<!-- docdaemon ts:2026-10-29T20:36:17Z hash:21a75211ddfb8570 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file centralizes all naming conventions, labels, and mount paths used by the WeftLocalDNSReflector to ensure consistency across the reconciler, cache filters, and tests.

### Purpose
The code defines constants for Kubernetes object names, labels, and annotations specific to the reflector. By keeping these in a single location, it prevents drift between the controller logic, the main package’s cache filter configuration, and test assertions.

### Key Constants
*   **Labels**: `ReflectorLabel` identifies objects owned by a specific reflector instance. `nameLabel` (aliased from `trust.ManagedLabel`) is critical for the manager cache to watch only relevant ConfigMaps. `createdByLabel` attributes ownership to the operator.
*   **Finalizer**: `finalizer` ensures the reflector’s key is removed from the shared CoreDNS ConfigMap upon deletion, as the reflector cannot use standard ownerReferences for this resource.
*   **Annotations & Paths**: `certFingerprintAnnotation` triggers proxy restarts on certificate rotation. Mount paths (`tlsMountPath`, `caMountPath`, `hostEtcPath`) define where secrets and trust bundles are exposed to the proxy and node agent pods.

### Functions
*   **Naming Helpers**: `caSecretName`, `tlsSecretName`, `proxyName`, and `nodeAgentName` generate deterministic names based on the reflector's name.
*   **CoreDNS Integration**: `coreDNSKey` generates the specific key (with `.server` suffix) used to inject reflector routes into the global CoreDNS ConfigMap.
*   **Label Management**: `managedLabels` returns the full set of metadata labels for new resources, while `selectorLabels` returns the immutable subset required for Deployment/DaemonSet selectors and Services.