<!-- docdaemon ts:2026-10-29T20:36:15Z hash:ed1fcaf41974312b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the conflict-resolution logic that determines which `WeftLocalDNSReflector` instance is responsible for serving specific hostnames.

### Purpose
The system prevents duplicate CoreDNS zones and agent conflicts by ensuring each hostname is served by exactly one reflector. It uses a deterministic "oldest wins" strategy based on creation time and name to break ties.

### Key Functions

*   **`resolveClaims`**: The main entry point. It filters out deleted reflectors, sorts them by age (oldest first), and assigns ownership. Explicit `hostnames` claims take precedence over `allHostnames`. If the self-referencing reflector loses a claim, it is added to `conflicting`; otherwise, it goes to `claimed`.
*   **`tunnelHostnames`**: Extracts valid, unique HTTP/HTTPS destination hostnames from `WeftTunnel` resources, filtering out invalid schemes and IP literals.
*   **`certNames`**: Generates SAN entries for certificates. Returns a placeholder domain if no hostnames are claimed, ensuring a loadable certificate always exists.
*   **`older`**: Comparison helper that sorts reflectors by `CreationTimestamp`, treating zero times as "newest" (last). Ties are broken alphabetically by name.
*   **`sortedUnique`**: Deduplicates and lowercases a string slice, returning a sorted result.

### Design Notes
The `cluster-default` reflector (introduced in recent updates) utilizes the `allHostnames` mode to capture any tunnel-hosted service not explicitly claimed by another reflector, keeping traffic in-cluster without per-cluster configuration.