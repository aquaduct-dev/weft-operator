<!-- docdaemon ts:2026-10-30T02:59:25Z hash:5aa3cc76b1e1f5b6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains an end-to-end test that validates the fake AquaductTaaS server correctly implements the HTTP protocol expected by the real operator client.

The `TestFakeSpeaksTheClientsProtocol` function spins up a `httptest.Server` backed by the internal `fake` struct and connects it using the production `aquaducttaas.NewHTTPAPIClient`. This ensures the in-memory mock accurately reflects the wire format, preventing VM runs from failing due to protocol mismatches.

Key behaviors verified include:
*   **Authentication**: Rejects invalid access tokens and handles JWT revocation/exchange logic.
*   **Bastion Management**: Lists external servers, formats connection strings (`weft://...`), and suspends bastions.
*   **Domain Operations**: Creates, patches (including empty/null handling), looks up, and deletes domains.
*   **Access Control**: Enforces ownership checks, returning `ErrDomainForeign` for operations on domains owned by other entities.