<!-- docdaemon ts:2026-10-29T20:35:41Z hash:c7d47c69118e605a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This workflow triggers on successful test runs to build, containerize, and publish release artifacts for the project.

### Purpose
The `Release` job executes only when the preceding `Test` workflow completes successfully on the `main` branch. It handles the end-to-end publishing process for both binary distributions and container images.

### Key Steps
1. **Environment Setup**: Restores Bazel caches using a key derived from the previous run number to optimize build times. Sets up Java 17 (required by Bazel) and installs Bazelisk.
2. **Build & Verify**: Compiles binaries for AMD64 and ARM64 architectures using Bazel targets `//cmd:cmd` and `//cmd:cmd_arm64`. A sanity check ensures the resulting binaries match their expected architectures.
3. **Publishing**:
   - Logs into GitHub Container Registry (GHCR) using the `GITHUB_TOKEN`.
   - Pushes multi-architecture container images tagged with `v1.1.<run_number>` and `latest`.
   - Pushes Helm charts via Bazel.
   - Creates an annotated Git tag and a GitHub Release containing the built binaries.
4. **Cache Management**: Saves the Bazel cache for future runs using the current run number as the key.

### Configuration Notes
- **Bazelisk Version**: The installation URL is pinned to v1.25.0 to avoid resolution failures associated with older versions.
- **Permissions**: Requires `contents: write` and `packages: write` to create tags, releases, and push images.