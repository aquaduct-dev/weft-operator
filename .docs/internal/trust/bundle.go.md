<!-- docdaemon ts:2026-10-29T20:36:46Z hash:da0a32ca76ac0df1 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file assembles a combined trust bundle of system and reflector CAs, manages the corresponding ConfigMap, and provides fingerprint data for pod injection.

### Purpose
The `trust` package ensures workloads trust WeftLocalDNSReflector CAs without workload changes. It builds a shared bundle (system roots + reflector CAs) and publishes it as the `weft-trust-bundle` ConfigMap. The webhook (`inject.go`) mounts this ConfigMap into pods at creation time.

### Key Types & Functions

*   **`Build`**: Assembles the `Bundle` by concatenating system roots with sorted reflector CAs. It enforces a minimum certificate count (`minSystemRoots`) to prevent publishing a broken bundle if system roots are missing or truncated.
*   **`Source`**: Aggregates CA data from the cluster.
    *   `CAs`: Lists active reflectors and retrieves their CA Secrets (`<name>-ca`). Skips reflectors marked for deletion or with invalid/missing secrets.
    *   `Build`: Combines `CAs` with the local system root file to produce a complete `Bundle`.
*   **`EnsureConfigMap`**: Creates or updates the trust bundle ConfigMap in a namespace. It sets ownership references to the reflectors carrying the CAs, enabling garbage collection when they are removed.
*   **`CreateConfigMapIfMissing`**: A safety check used by the reconciler/webhook to create the ConfigMap only if it doesn't exist, preventing race conditions over content or ownership.
*   **`OwnerRefs`**: Generates Kubernetes owner references for reflectors whose CAs are present in the bundle, ensuring proper lifecycle management.

### Configuration
*   **`DefaultSystemRootsFile`**: Path to the system CA bundle in the operator image (`/etc/ssl/certs/ca-certificates.crt`).
*   **`ManagedLabel`**: Labels applied to the ConfigMap to allow filtering by the informer.