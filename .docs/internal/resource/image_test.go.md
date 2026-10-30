<!-- docdaemon ts:2026-10-30T02:59:58Z hash:18a3ad24381c313e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the image policy and default image resolution logic within the resource package.

### Purpose
The tests verify the correctness of `PullPolicy` and `WeftImageOr` functions, ensuring that image references are correctly interpreted for pull strategies and that fallback defaults are applied appropriately.

### Key Functions

#### `TestPullPolicy`
Validates the `PullPolicy` function against a variety of image reference formats:
- **Latest/No Tag**: Images like `ghcr.io/aquaduct-dev/weft:latest` or those without explicit tags resolve to `Always`.
- **Localhost**: References to local registries (e.g., `localhost:5000`) also resolve to `Always`.
- **Specific Tags/Digests**: Images with specific version tags (e.g., `v1`, `e2e`) or SHA256 digests resolve to `IfNotPresent`.

This behavior ensures that development and local testing scenarios prioritize fetching new images, while production-like or pinned references avoid unnecessary pulls.

#### `TestWeftImageOr`
Verifies the `WeftImageOr` helper function:
- Returns `DefaultWeftImage` when an empty string is provided.
- Returns the input string unchanged if it is non-empty.

This pattern allows callers to easily provide a custom image name while falling back to a sensible default if none is specified.