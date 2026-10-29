<!-- docdaemon ts:2026-10-29T20:36:21Z hash:788686e396a6783a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script installs a Git pre-commit hook that automatically runs Gazelle and Bazel tests before each commit.

The script creates the file `.git/hooks/pre-commit` with executable permissions. The installed hook enforces two checks:
1. **Gazelle Sync**: It runs `bazel run //:gazelle` to regenerate Bazel build files, ensuring they stay in sync with source code changes.
2. **Test Execution**: It runs `bazel test --build_tests_only --test_tag_filters=-requires-kvm //...`. This executes all unit and integration tests except those requiring KVM (e.g., firecracker e2e tests), matching the filter used in CI pipelines.

The hook uses `set -e` to abort immediately if any command fails, preventing commits that break the build or leave build files out of date.