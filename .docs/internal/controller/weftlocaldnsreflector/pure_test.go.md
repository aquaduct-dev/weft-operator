<!-- docdaemon ts:2026-10-29T20:35:44Z hash:06785cd2b8148b52 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the pure helper functions that derive DNS routes from WeftTunnels and render CoreDNS configuration blocks.

### Purpose
The tests verify `computeRoutes`, which maps `WeftTunnel` resources to destination hostnames, handling scheme normalization (e.g., HTTPS on port 8443) and conflict resolution where multiple tunnels target the same destination. It also validates `renderCoreDNS`, ensuring the generated CoreDNS proxy configuration is syntactically correct, sorted, and includes necessary directives like `fallthrough` and `forward`.

### Key Functions Tested
*   **`TestComputeRoutes`**: Confirms that route selection prioritizes specific claims over generic ones, skips invalid or unreachable targets, and correctly generates proxy arguments with TLS certificate paths.
*   **`TestRenderCoreDNS`**: Ensures the renderer rejects invalid inputs (bad IPs, malformed hosts) and produces balanced, sorted block structures for each requested hostname.
*   **`TestPullPolicy`**: Validates image pull policy logic based on tag presence and digest usage.
*   **`TestResolveClaims`**: Tests the complex logic for resolving ownership of hostnames among multiple `WeftLocalDNSReflector` objects, accounting for deletion timestamps, `allHostnames` mode precedence, and placeholder SAN generation for empty claim sets.