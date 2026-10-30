<!-- docdaemon ts:2026-10-30T02:59:53Z hash:1ffbe77090358a07 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `probedIP` function, which extracts valid public IP addresses from probe log output.

The test suite validates that `probedIP` correctly parses various log formats, including zerolog console output with ANSI escape codes and plain text logs. It ensures the function returns the host address only when an "ACME probe succeeded" message is present.

Key validation scenarios include:
- **Format Handling**: Correctly strips color codes and whitespace to extract IPs from structured logs.
- **Success Criteria**: Returns empty string if no success line exists or if the probe fails.
- **Address Validation**: Filters out private IP ranges (e.g., `10.0.0.1`) and non-IP hosts (e.g., `example.com`), ensuring only public IPv4 addresses are returned.
- **Edge Cases**: Handles empty input and bare address lines gracefully.