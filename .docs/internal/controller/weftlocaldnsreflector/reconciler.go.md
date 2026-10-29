<!-- docdaemon ts:2026-10-29T20:35:59Z hash:44e75271765e9867 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the controller that routes in-cluster traffic to public Weft hostnames via a local proxy, ensuring TLS trust and DNS consistency.

The `Reconciler` type drives the desired state for `WeftLocalDNSReflector` custom resources. It coordinates five key subsystems: certificate issuance, proxy availability, client trust validation, CoreDNS configuration, and node agent deployment.

### Key Functions

*   **`Reconcile`**: The entry point. It handles finalization on deletion, applies defaults, and delegates to `reconcile`. It manages status updates with conflict resolution to ensure eventual consistency.
*   **`reconcile`**: The core logic loop. It computes hostname-to-tunnel mappings, ensures the CA and serving certificates are valid, and publishes the trust bundle. It enforces a "trust gate": DNS overrides and node agents only activate if all running pods trust the CA. If untrusted pods exist, it requeues frequently until they restart or are ignored by spec.
*   **`ensureCertificates`**: Manages PKI lifecycle, issuing certificates covering claimed hostnames and setting conditions based on expiry.
*   **`syncCoreDNS`**: Updates the shared CoreDNS ConfigMap. A finalizer ensures this key is removed upon reflector deletion to prevent orphaned DNS entries.
*   **`ensureProxy`**: Creates/updates the Deployment and Service for the `weft proxy` sidecar, which terminates TLS and forwards traffic through WeftTunnels.
*   **`ensureNodeAgent`**: Deploys a DaemonSet to update `/etc/hosts` on nodes, allowing host processes to resolve proxied hostnames directly.
*   **`SetupWithManager`**: Registers watches for reflectors, tunnels, namespaces, and owned resources (Deployments, Services, Secrets, ConfigMaps) to trigger reconciliation on relevant changes.

### Design Notes

The controller uses a fan-out strategy (`allReflectors`) because hostname claims depend on the state of all other reflectors and tunnels. Changes to any tunnel or reflector can shift ownership, requiring all reflectors to re-evaluate their routes. Trust validation is critical; the system withholds DNS overrides if pods lack the CA bundle, preventing TLS handshake failures during rollout.