<!-- docdaemon ts:2026-10-29T20:36:06Z hash:06422fc422c803ec model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the domain model and HTTP client interface for managing aquaduct.dev domain registrations.

It exposes `DomainAPIClient` to abstract API interactions, allowing the DNSRecord reconciler to manage domain ownership without coupling to bastion logic. Key types include `Domain` (the operator-facing state) and wire-format structs like `apiDomain`.

The implementation handles three critical reconciliation states:
*   **Ownership**: `PatchDomain` asserts write access; 401/403/409 errors surface as `ErrDomainForeign`, indicating another user owns the record.
*   **Creation**: `PutDomain` is idempotent; it creates or updates the domain binding.
*   **Resolution**: `LookupDomain` fetches current A-record IPs from Cloudflare via the server.

Error handling distinguishes between transient network issues (retryable) and durable conditions like `ErrDomainNotFound` (safe to create) or `ErrDomainForeign` (requires operator intervention). The client normalizes integer IDs to strings for status fields and handles edge cases like empty PUT responses.