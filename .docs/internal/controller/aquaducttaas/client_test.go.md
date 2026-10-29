<!-- docdaemon ts:2026-10-29T20:36:04Z hash:fe3e35f4445a62be model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file tests the `HTTPAPIClient` to ensure it correctly handles two-hop authentication, JWT caching, and API interactions with the AquaductTaaS service.

It verifies that the client exchanges a long-lived access token for a short-lived JWT at `/api/auth/access-token` and uses that JWT for subsequent bastion operations. The tests confirm proactive refresh logic based on expiration times and automatic retry on 401 errors to handle token rotation. It also validates error handling for invalid responses, malformed JSON, and fallback to default endpoints.