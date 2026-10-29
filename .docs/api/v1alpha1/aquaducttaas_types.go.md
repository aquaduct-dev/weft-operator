<!-- docdaemon ts:2026-10-29T20:35:40Z hash:d2364d5c49639143 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Custom Resource Definition (CRD) types for the `AquaductTaaS` operator, establishing the schema for managing cloud-hosted bastion infrastructure.

The `AquaductTaaS` struct serves as the primary API object, linking a Kubernetes cluster to an external aquaduct.dev account. Its `Spec` requires an `AccessTokenSecretRef` pointing to a Secret containing long-lived authentication credentials. The reconciler uses this reference to exchange tokens for short-lived JWTs, enabling API calls to provision and mirror bastions as `WeftServer` objects with `Location=External`.

The `BastionInfo` struct captures the state of individual bastions, including their ID, IP address, and suspension status. This data is published in the `AquaductTaaSStatus.Bastions` field, providing a canonical source of truth for downstream controllers like `DNSRecord` to compute fan-out routing decisions without querying the external API directly.

Key components include:
*   **AquaductTaaSSpec**: Defines desired configuration, including optional API endpoint overrides.
*   **AquaductTaaSStatus**: Tracks synchronization time, mirrored server names, and the full list of accessible bastions.
*   **Scheme Registration**: Initializes the Kubernetes scheme builder to register these types for API discovery.