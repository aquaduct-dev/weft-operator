<!-- docdaemon ts:2026-10-30T02:59:25Z hash:5ab1052b9069e3d9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the step functions for testing DNS record lifecycle and conflict resolution in the Aquaduct e2e suite.

It verifies `DNSRecord` creation, registration with a fake domain API, and status transitions. Key scenarios include:

*   **Registration**: Ensures new domains trigger a GET (404) then PUT (201), while pre-existing owned domains use GET/PATCH and set `ClobberedPreexisting`.
*   **Conflict Handling**: Validates rejection of foreign-owned domains (403) and contested writes (409), including recovery after conflicts resolve.
*   **Target Management**: Tests pinned bastion targets, unknown target errors, and handling of suspended/unsuspended bastions (empty vs. populated `bastion_ids`).
*   **Deletion**: Confirms proper cleanup via DELETE requests and finalizer removal.

The `checkRegistered` helper validates specific HTTP sequences against recorded requests to ensure correct API interaction.