<!-- docdaemon ts:2026-10-30T02:59:29Z hash:c9f828b58549334d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements end-to-end verification steps for Gateway API resources and their interaction with the AquaductTaaS DNS management system.

It defines helper functions to construct `Gateway` and `Listener` objects, and provides scenario methods that validate the lifecycle of DNS records associated with Gateway hostnames. Key behaviors include:

*   **Record Creation & Validation**: `checkGatewayRecord` ensures a `DNSRecord` is created with correct labels, domain name, and references to the `AquaductTaaS` instance.
*   **Lifecycle Management**: Methods like `checkGatewayRecords`, `checkListenerPruned`, and `checkGatewayDeleted` verify that DNS records are created when Gateways are added, removed when listeners are pruned, and cleaned up entirely when the Gateway is deleted.
*   **Constraint Enforcement**: `checkL4HostnameRejected` confirms that TCP listeners cannot specify hostnames, enforcing L4 bastion pinning rules.
*   **Deletion Order**: `checkDeleteOrder` validates that when `AquaductTaaS` is deleted, domains are unregistered via DELETE requests before bastions are suspended, ensuring no orphaned registrations remain.
*   **Secret Handling**: `checkSecretPinned` verifies that authentication secrets retain finalizers during deletion to prevent premature cleanup.