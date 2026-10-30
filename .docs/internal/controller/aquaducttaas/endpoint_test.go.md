<!-- docdaemon ts:2026-10-30T02:59:46Z hash:1f5f73e5e6ede098 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `aquaducttaas` HTTP API client, verifying endpoint overrides, response handling, and error semantics.

### Purpose
The tests ensure the `HTTPAPIClient` correctly routes requests to configured endpoints and interprets HTTP responses according to domain rules. It uses a lightweight fake server to simulate the AquaductTaaS backend without network dependencies.

### Key Components

*   **`fakeAPI`**: A helper struct wrapping `httptest.Server`. It intercepts `/api/auth/access-token` to return a static JWT, allowing authentication to be bypassed in tests. All other request paths are recorded for verification.
*   **`TestWithEndpointOverridesTheDefault`**: Validates that `WithEndpoint` context option successfully redirects a specific call (`ListExternalServers`) to an override URL while leaving subsequent calls on the default URL. It also verifies trailing slash normalization.
*   **`TestDomainWritesAcceptAnEmptyBody`**: Confirms that `PutDomain` and `PatchDomain` succeed when the server returns `204 No Content`, ensuring the client handles empty bodies correctly for write operations.
*   **`TestGetDomainForbiddenIsForeign`**: Ensures that a `403 Forbidden` response from `GetDomain` is mapped to `ErrDomainForeign`, distinguishing ownership errors from transient failures.