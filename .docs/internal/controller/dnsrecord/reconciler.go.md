<!-- docdaemon ts:2026-10-29T20:36:09Z hash:6c974316d323a0f3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `DNSRecord` controller, reconciling Kubernetes custom resources with domain registrations on the AquaductTaaS API.

The `DNSRecordReconciler` drives domains toward a desired state by resolving an `AquaductTaaS` dependency for credentials and bastion IP lists. It manages the lifecycle through a finalizer that ensures cleanup on deletion. The reconciliation loop follows a strict sequence: it bootstraps dependencies, computes expected IPs from bastions, and then interacts with the external API via PUT (for new domains) or PATCH (for updates). It handles ownership conflicts (`ErrDomainForeign`) gracefully, allowing records to be marked as externally managed if DNS matches expectations despite lacking write access.

Status conditions track three aspects of health:
*   **Registered**: Whether the operator successfully claimed or updated the domain record.
*   **Resolved**: Whether the actual DNS A-records match the computed expected IPs.
*   **Ready**: Mirrors Resolved, indicating end-to-end correctness regardless of ownership.

To prevent watch loops caused by status updates triggering re-reconciles, `commitStatus` only writes when semantic changes occur or the heartbeat is stale. Bastion selection logic filters suspended nodes and validates target IDs against the published list, ensuring deterministic IP sets.