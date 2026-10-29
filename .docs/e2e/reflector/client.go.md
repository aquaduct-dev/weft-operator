<!-- docdaemon ts:2026-10-29T20:35:55Z hash:229c902c9c7c4a70 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the e2e client workload that validates DNS reflection and TLS trust establishment by the reflector.

The `runClient` function serves as the entry point, accepting flags to define expected outcomes (resolution success/failure, certificate trust). It performs a DNS lookup for the target host, verifying the IP matches expectations if specified. Subsequently, it issues an HTTPS GET request to `/v2/` using the system's default root CAs, ensuring no custom certificates are injected into the transport. This verifies that the trust webhook correctly populated the system store. If the `push` flag is set, it uploads a minimal OCI image containing its own binary to the registry, exercising the full push workflow under the reflected identity.

Key components include:
- **Validation Logic**: Checks DNS resolution against expected IPs or unresolvability states. Validates HTTP responses for standard Docker Registry API headers.
- **Image Pushing**: `pushSelf` constructs a one-layer OCI image in memory, computing SHA256 digests for layers and config. It uses the direct HTTP API to initiate an upload session, push the blob, and finalize the manifest.
- **HTTP Helpers**: `expect` provides a functional wrapper for asserting specific HTTP status codes, simplifying error handling during upload operations.