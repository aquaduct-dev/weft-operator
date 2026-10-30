<!-- docdaemon ts:2026-10-30T02:59:59Z hash:8047364b27eae34d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file assembles a combined trust bundle of system and reflector CAs, manages the corresponding ConfigMap, and validates namespace integrity.

### Purpose
The `trust` package ensures workloads trust WeftLocalDNSReflector CAs without workload changes. It builds a shared bundle (system roots + reflector CAs) and publishes it as a ConfigMap (`weft-trust-bundle`). The `inject.go` module mounts this ConfigMap into pods via a webhook.

### Key Types & Functions

*   **`Bundle`**: Holds the combined CA data (`All`), reflector-only CAs (`CAs`), and sorted fingerprints. `Empty()` checks if no reflectors are present.
*   **`Build`**: Assembles the bundle. It validates that system roots contain at least 20 certificates to prevent breaking TLS connections due to truncated or missing system bundles.
*   **`Source`**: Aggregates CA data from cluster reflectors and reads system roots from disk.
    *   `CAs`: Lists active reflectors and retrieves their CA Secrets. Skips reflectors being deleted or with missing/invalid secrets.
    *   `Build`: Combines live cluster CAs with local system roots.
*   **ConfigMap Management**:
    *   `EnsureConfigMap`: Creates or updates the trust bundle ConfigMap, marking it with managed labels for garbage collection.
    *   `CheckConfigMap`: Verifies that no unmanaged ConfigMap with the same name exists in the namespace, preventing accidental overwrites of external trust stores.
    *   `CreateConfigMapIfMissing`: Atomically creates the ConfigMap only if it doesn't exist, handling race conditions with the reconciler.
*   **`OwnerRefs`**: Generates owner references for the ConfigMap based on active reflectors, ensuring cleanup when reflectors are removed.

### Integration
The reconciler uses `Source.Build` and `EnsureConfigMap` to maintain the bundle state. The webhook uses `CheckConfigMap` and `CreateConfigMapIfMissing` to ensure safe injection. Labels (`app.kubernetes.io/name=weft-local-dns-reflector`) allow the operator to filter and manage these ConfigMaps exclusively.