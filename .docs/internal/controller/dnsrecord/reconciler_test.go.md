<!-- docdaemon ts:2026-10-30T02:59:48Z hash:895df47398515c18 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains integration tests for the DNSRecord controller, verifying domain lifecycle management and status condition transitions.

The `mockDomainClient` implements the external API interface, allowing tests to simulate server responses (GET, PUT, PATCH, DELETE) and track call counts. Helper functions like `createTaaS` and `seedTaaSBastions` set up the necessary Kubernetes resources and bastion topology before each test case.

Tests cover:
- **Happy Paths**: Initial registration, ownership assertion via PATCH, and idempotent resyncs that avoid unnecessary status writes.
- **Error Handling**: Recovery from transient API errors, handling of foreign-owned domains (`ForeignOwned`, `ExternallyManaged`), and missing dependencies (`MissingAquaductTaaS`, `SecretError`).
- **Deletion**: Finalizer logic ensuring `DeleteDomain` is called on cleanup, with retries on failure and graceful handling of 404s.
- **Validation**: CEL immutability rules preventing spec changes after creation.