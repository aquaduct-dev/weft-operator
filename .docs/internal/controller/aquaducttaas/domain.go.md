<!-- docdaemon ts:2026-10-30T02:59:46Z hash:edbb733d3aee0eac model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the domain model and HTTP client implementation for managing aquaduct.dev domain registrations.

The `Domain` struct mirrors the server's API response, converting internal integer IDs to strings for Kubernetes status fields. The `DomainAPIClient` interface abstracts these operations, allowing the DNSRecord reconciler to mock dependencies without stubbing bastion logic.

Key methods include:
- **GetDomain**: Retrieves domain state; returns `ErrDomainNotFound` on 404 or `ErrDomainForeign` on 403.
- **PutDomain**: Registers a domain with specific bastion bindings. It is idempotent but allows "clobbering" existing records.
- **PatchDomain**: Asserts ownership by sending a minimal PATCH request. This mechanism was introduced to handle concurrent ownership conflicts (409) and foreign-owned domains (401/403), surfacing them as `ErrDomainForeign` so the reconciler can set appropriate status conditions rather than retrying indefinitely.
- **DeleteDomain**: Removes the domain registration. A 404 is treated as success, while ownership errors are surfaced to allow finalizer cleanup.
- **LookupDomain**: Queries Cloudflare A-records to verify resolution.

The implementation uses pointer semantics (`*[]string`) for `bastionIDs` in payloads to distinguish between "omit field" (preserve existing) and "empty list" (clear bindings).