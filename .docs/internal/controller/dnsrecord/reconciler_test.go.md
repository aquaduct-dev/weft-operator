<!-- docdaemon ts:2026-10-29T20:36:09Z hash:097806a1ea5f9b21 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains integration tests for the `DNSRecord` controller, verifying domain registration, ownership clobbering, resolution checks, and finalizer-based cleanup.

The test suite uses Ginkgo/Gomega with an `envtest` environment to validate the reconciler's lifecycle. A `mockDomainClient` implements the external API interface, allowing tests to simulate success, failure, race conditions (e.g., deletion during PATCH), and state changes via configurable function hooks.

Key scenarios covered include:
*   **Happy Paths**: Initial registration (PUT), idempotent resyncs (no status write if unchanged), and successful resolution verification.
*   **Ownership & Clobbering**: Handling pre-existing domains via PATCH, detecting foreign-owned records (`ErrDomainForeign`), and distinguishing between "externally managed but correct" vs "mismatched" states.
*   **Error Recovery**: Retrying on transient API errors, handling lookup failures, and recovering from server-side record deletion.
*   **Deletion**: Verifying that the `unregister-on-delete` finalizer triggers `DeleteDomain` calls and is removed only after successful cleanup or when credentials are missing.
*   **Validation**: Ensuring `spec.domainName` remains immutable via CEL rules.