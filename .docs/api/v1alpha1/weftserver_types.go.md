<!-- docdaemon ts:2026-10-29T20:35:46Z hash:6afe520a81c00489 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Custom Resource Definition (CRD) types for the WeftServer API, establishing the schema for managing server configurations and tunnel statuses.

The `WeftServer` struct serves as the root object, embedding standard Kubernetes metadata (`TypeMeta`, `ObjectMeta`) alongside its specific `Spec` and `Status`. It is registered in the scheme via `init()` to be recognized by the controller-runtime manager.

**Key Types:**

*   **`WeftServerSpec`**: Defines the desired state.
    *   `ConnectionString`: The connection string for the server.
    *   `Location`: Enum indicating if the server is `Internal` or `External` to the cluster. Defaults to `Internal`.
    *   `BindInterface`: Specifies the network interface to bind.
    *   `UsageReportingURL`: URL for reporting usage metrics.
    *   `CloudflareTokenSecretRef`: A reference to a `corev1.SecretKeySelector` containing the Cloudflare API token. This field securely references credentials rather than storing them directly.
*   **`TunnelStatus`**: Represents metrics for an active tunnel, including transmit (`Tx`) and receive (`Rx`) byte counts, and source/destination URLs.
*   **`WeftServerStatus`**: Holds observed state, specifically a list of `Tunnels` and `Conditions` tracking the current health and status of the resource.

The file uses kubebuilder markers (`+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`) to generate the necessary CRD manifests and enable the status subresource endpoint.