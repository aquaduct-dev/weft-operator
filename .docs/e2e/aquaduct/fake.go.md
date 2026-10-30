<!-- docdaemon ts:2026-10-30T02:59:26Z hash:71f71081e26f3759 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements an in-memory HTTP server that mimics the aquaduct.dev API and authentication service for end-to-end testing.

The `fake` struct holds mutable state (bastions, domains, JWTs) protected by a mutex. It serves two distinct namespaces:

*   **API Endpoints (`/api/*`)**: Replicates real behavior. The `/api/auth/access-token` endpoint validates the configured access token and mints short-lived JWTs. Subsequent requests require a valid JWT. Domain operations enforce ownership checks (only "me" can modify), handle pointer-slice semantics for bastion assignment, and support fault injection via configurable status codes.
*   **Control Endpoints (`/_fake/*`)**: Provides introspection and setup for tests. Allows resetting the bastion list, seeding domains, overriding DNS lookups, injecting transient faults, revoking all JWTs, and retrieving the ordered request log.

Key types include `fakeBastion`, `fakeDomain`, and `fakeFault`. The `ServeHTTP` method routes requests, logs activity to stdout, and ensures thread-safe access to shared state. This allows e2e tests to interact with a deterministic, isolated version of the external service without network dependencies.