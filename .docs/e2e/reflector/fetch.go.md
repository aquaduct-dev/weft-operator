<!-- docdaemon ts:2026-10-30T02:59:36Z hash:065fff4208272d9b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the "fetch" mode for end-to-end testing, performing a plain HTTP GET against a reflected URL to verify registry connectivity.

### Purpose
The `runFetch` function executes inside a pod to validate that routes publishing an `http://` destination are correctly served by the proxy's HTTP port. It ensures the reflector is functioning as expected for specific registry interactions.

### Key Logic
1.  **Argument Parsing**: Accepts a target `http` URL and an optional `expect-ip` to enforce DNS resolution constraints.
2.  **DNS Verification**: Resolves the target hostname using the default resolver. If `expect-ip` is provided, it verifies the resolved address matches the expectation, failing if there is a mismatch or resolution error.
3.  **HTTP Request**: Performs a GET request to the target URL with a one-minute timeout.
4.  **Response Validation**: The test passes only if:
    *   The response status code is `200 OK`.
    *   The `Docker-Distribution-Api-Version` header equals `registry/2.0`.

This strict validation ensures that the reflected route correctly serves the registry's `/v2/` endpoint, confirming both network reachability and protocol compliance.