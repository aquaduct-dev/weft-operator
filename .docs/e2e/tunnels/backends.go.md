<!-- docdaemon ts:2026-10-30T02:59:42Z hash:e24de8d80f03de7f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides the echo backend servers and node-side client helpers used to validate traffic flow through Weft tunnels in end-to-end tests.

### Backends
*   **`runEcho`**: Starts HTTP, TCP, and UDP listeners that echo back request details or raw bytes. It serves as the destination for verifying tunnel correctness.
*   **`runACMEStandin`**: A minimal HTTP server returning 404s, used to simulate external services during ACME challenge validation tests.

### Node-Side Clients
These functions simulate traffic from the test node to verify tunnel behavior:

*   **`httpVia`**: Sends an HTTP GET request with a specific `Host` header and custom headers. It disables connection keep-alives to ensure each probe uses a fresh route through the current tunnel configuration. It parses the JSON response into an `echoReply` struct containing method, host, path, and headers.
*   **`tcpEcho`**: Dials a TCP address, sends random payload bytes, and verifies they are returned intact. It handles concurrent read/write operations and respects context deadlines.
*   **`udpEcho`**: Sends a single UDP datagram and verifies the response matches the sent payload exactly.
*   **`refused`**: Checks if a TCP port is actively refusing connections, useful for verifying service teardown.
*   **`header`**: Helper to extract canonical header values from an `echoReply`.