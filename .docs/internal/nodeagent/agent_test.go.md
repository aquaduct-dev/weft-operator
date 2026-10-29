<!-- docdaemon ts:2026-10-29T20:36:27Z hash:0ef3eb26508d6da8 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the node agent, verifying its logic for managing `/etc/hosts` entries, containerd certificate trust configurations, and health-gated synchronization cycles.

### Test Coverage

*   **Hosts Block Management (`TestWithHostsBlock`)**: Validates `withHostsBlock` for idempotent appending, in-place replacement of existing blocks, preservation of unrelated lines, and safe handling of malformed (unterminated) blocks.
*   **Certificate Ownership (`TestCertsDOwnership`)**: Ensures `writeCertsD` and `removeCertsD` correctly manage ownership tags, preventing overwrites or deletions of directories belonging to other reflectors or foreign services.
*   **Agent Lifecycle (`TestAgentTickAndCleanup`)**: Exercises the full Tick/Cleanup loop against a temporary filesystem. It verifies that healthy agents add hosts entries and extend CA bundles, while unhealthy agents withdraw hosts entries but preserve trust files. It also confirms pruning of stale hostnames during cleanup.
*   **Configuration Parsing (`TestParseFlags`)**: Checks that flag parsing correctly normalizes comma-separated hostnames and rejects invalid IP addresses.