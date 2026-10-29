<!-- docdaemon ts:2026-10-29T20:35:46Z hash:bb53d581819b36fc model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Custom Resource Definition (CRD) types for managing DNS domain registrations via the aquaduct.dev API.

### Types

**DNSRecord**
The top-level resource representing a desired domain registration. It is scoped to a tenant and triggers reconciliation to ensure the domain exists at the external provider.

**DNSRecordSpec**
Defines the desired state:
*   `domainName`: The FQDN to register. This field is immutable; changes require deleting and recreating the resource to allow finalizers to release the old name.
*   `aquaductTaaSRef`: References a `LocalObjectReference` in the same namespace containing credentials and endpoint configuration for the AquaductTaaS service.
*   `targetBastionIDs`: Optional list of bastion IDs for fan-out routing. If empty, the system defaults to all non-suspended bastions.

**DNSRecordStatus**
Observed state from the aquaduct.dev API:
*   `Conditions`: Tracks `Registered` (exists on server), `Resolved` (DNS lookup matches expected IPs), and `Ready` (both true).
*   `DomainID`: Server-assigned identifier, cleared if the record no longer exists.
*   `ExpectedIPs` vs `ResolvedIPs`: `ExpectedIPs` are computed locally from bastion data; `ResolvedIPs` are actual DNS lookups. Discrepancy indicates propagation issues.
*   `AppliedIPs`/`AppliedBastionIDs`: State reported by the server immediately after PUT/PATCH, useful for verifying intent before DNS propagation.
*   `ClobberedPreexisting`: Flag indicating if the operator took over an existing domain rather than creating a new one.
*   `LastSyncTime`: Timestamp of the last successful round-trip with the API.

### Integration
The reconciler uses this spec to call `PUT /domain/{name}` and cleans up via `DELETE` on deletion. It drives the status conditions by comparing local expectations against server responses and live DNS lookups.