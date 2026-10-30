<!-- docdaemon ts:2026-10-30T02:59:49Z hash:6524e24ccf0a1e54 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `DNSRecordReconciler` to manage domain registrations and DNS resolution on aquaduct.dev.

The reconciler drives a `DNSRecord` resource toward a desired state by coordinating with an `AquaductTaaS` instance for credentials and bastion IP data. It follows a strict stage sequence: handling deletion via finalizers, resolving dependencies (AquaductTaaS and token), computing expected IPs from bastions, and then interacting with the aquaduct.dev API.

Key behaviors include:
*   **Registration**: Uses PUT/PATCH to register or update domain ownership, handling "foreign-owned" errors gracefully by checking if DNS is already correct (`ExternallyManaged`).
*   **Resolution**: Compares server-side resolved IPs against computed expected IPs to set the `Resolved` condition.
*   **Status Management**: The `commitStatus` helper prevents unnecessary API writes by comparing the new status against the previous snapshot, excluding only the heartbeat timestamp. This avoids re-triggering its own watches during resyncs.
*   **Deletion**: The `handleDeletion` method ensures the domain is unregistered on aquaduct.dev before removing the finalizer, retrying on transient failures.

It relies on `selectBastions` to deterministically map target bastion IDs to IPs and uses `stringSlicesEqual` for efficient comparison of sorted IP lists.