<!-- docdaemon ts:2026-10-30T02:59:44Z hash:17eb7cec989556d3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the HTTP client and data models for interacting with the aquaduct.dev API to manage external bastion servers.

`HTTPAPIClient` handles two-step authentication: it exchanges a long-lived access token for a short-lived JWT via `login`, caching the result in `getJWT`. The cache includes a grace period (`jwtExpiryGracePeriod`) to prevent expiry races, and automatically refreshes on 401 responses via `authedRequest`.

`ListExternalServers` fetches bastions from `/api/bastion`, mapping the API's `apiBastion` struct to the domain-specific `ExternalServer` type. It constructs `ConnectionString` using a default port (9092) since the API omits it. `SuspendServer` sends a PATCH request to pause a bastion.

The `APIClient` interface allows swapping this implementation for fakes in tests. Context values via `WithEndpoint` enable per-request API endpoint overrides, supporting multiple `AquaductTaaS` resources sharing a single client instance.