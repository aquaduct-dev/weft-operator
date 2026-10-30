<!-- docdaemon ts:2026-10-30T02:59:21Z hash:e23c4fab24648b02 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Custom Resource Definition (CRD) types for the `AquaductTaaS` operator, modeling the connection to the aquaduct.dev API and the state of mirrored bastions.

### Purpose
The `AquaductTaaS` resource acts as the bridge between the Kubernetes cluster and the user's hosted aquaduct.dev account. It allows the operator to authenticate via a long-lived token, exchange it for a short-lived JWT, and fetch cloud-hosted bastion information. This data is then materialized as `WeftServer` objects with `Location=External`, enabling other reconcilers (like `DNSRecord`) to treat external infrastructure uniformly.

### Key Types

*   **`AquaductTaaSSpec`**: Defines the desired configuration.
    *   `AccessTokenSecretRef`: References a Secret containing the long-lived authz access token. The operator exchanges this at `{AUTHZ_ENDPOINT}/api/auth/access-token` for a short-lived JWT used for API calls.
    *   `APIEndpoint`: Optional override for the aquaduct.dev API base URL. Defaults to `https://aquaduct.dev`.

*   **`BastionInfo`**: A snapshot of a single bastion reported by aquaduct.dev.
    *   Includes `ID` (stable opaque identifier), `Name`, `IP` (externally-routable IPv4), and `Suspended` status.
    *   Used by downstream reconcilers to compute IP/bastion associations without re-listing the API.

*   **`AquaductTaaSStatus`**: Represents the observed state.
    *   `Conditions`: Standard Kubernetes conditions.
    *   `LastSyncTime`: Timestamp of the last successful sync with aquaduct.dev.
    *   `SyncedServers`: List of `WeftServer` names currently mirrored from the API.
    *   `Bastions`: The canonical list of all accessible bastions (ID, IP, suspended state), serving as the source of truth for fan-out decisions.

*   **`AquaductTaaS` / `AquaductTaaSList`**: The main CRD struct and its list variant, registered with the scheme builder.