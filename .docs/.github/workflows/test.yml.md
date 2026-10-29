<!-- docdaemon ts:2026-10-29T20:35:42Z hash:f43e456bfc26ed62 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This GitHub Actions workflow orchestrates the CI test suite using Bazel, including cache management and conditional execution of KVM-dependent end-to-end tests.

### Workflow Overview
The `Test` job runs on `ubuntu-latest` and executes all Bazel tests (`//...`) excluding those tagged `requires-kvm`. It manages a persistent Bazel cache to speed up builds across runs.

### Key Steps

1.  **Cache Key Calculation**: Computes the previous run number to generate a deterministic cache key for restoring prior build artifacts.
2.  **Bazel Cache Restoration**: Restores the Bazel output base from `/home/runner/.cache/bazel` using the calculated key, with fallback keys to ensure partial cache hits.
3.  **Environment Setup**:
    *   Checks out the repository with full history (`fetch-depth: 0`) and Git LFS support.
    *   Installs Java 17 (Temurin distribution), which is required by Bazel.
    *   Installs Bazelisk v1.25.0 to manage the Bazel version dynamically based on `.bazelversion`.
4.  **Test Execution**: Runs `bazelisk test` with the following flags:
    *   `--test_output=streamed`: Streams test logs for real-time visibility.
    *   `--build_tests_only`: Skips building non-test targets to save time.
    *   `--test_tag_filters=-requires-kvm`: Excludes e2e tests that require KVM access, as these cannot run in this standard CI environment.
5.  **Cache Saving**: Saves the updated Bazel cache to `/home/runner/.cache/bazel` using the current run number as the key.