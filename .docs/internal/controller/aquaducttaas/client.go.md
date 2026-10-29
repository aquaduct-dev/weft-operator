<!-- docdaemon ts:2026-10-29T20:36:05Z hash:2ea2a47f3cd847e0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the HTTP client and data models for interacting with the aquaduct.dev API to manage external bastion servers.

`HTTPAPIClient` handles two-step authentication: it exchanges a long-lived access token for a short-lived JWT via an authz endpoint, caching the JWT with expiry logic (including a 30s grace period) to prevent spurious 401s. If a request fails with 401, the cache is cleared and the request is retried once to handle server-side token rotation.

The client exposes `ListExternalServers` and `SuspendServer`, mapping API responses into `ExternalServer` structs. It constructs `weft://` connection strings using a default port of 9092 when the API does not provide one. The `APIClient` interface allows swapping this implementation for fakes in tests.