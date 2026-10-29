<!-- docdaemon ts:2026-10-29T20:39:38Z hash:9faa99844b5ab827 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script automates the installation of a Git pre-commit hook to enforce build integrity and test coverage.

## Overview
The `setup-pre-commit-hook.sh` file provides a one-time setup mechanism for developers to ensure their local environment adheres to project standards before committing code. It generates a `.git/hooks/pre-commit` script that intercepts commit attempts, running essential validation steps to prevent broken builds or outdated configuration files from entering the repository. This ensures consistency between source code changes and Bazel build definitions, as well as immediate feedback on test failures.

## Deep Dive
### Hook Installation
When executed, the script writes a new shell script to `.git/hooks/pre-commit`. This target file is marked executable, allowing Git to invoke it automatically during the commit process. The script uses `set -e` to ensure that if any command within the hook fails, the entire hook execution stops immediately, causing the commit to abort. This strict failure mode prevents partial commits or commits that leave the repository in an inconsistent state.

### Gazelle Synchronization
The first check performed by the installed hook is `bazel run //:gazelle`. Gazelle is a tool that automatically generates and updates Bazel `BUILD.bazel` files based on the current source tree structure. By running this before every commit, the hook guarantees that build rules are always up-to-date with the latest source files. This eliminates common errors where new files are added but not included in the build, or deleted files remain referenced in build configurations.

### Test Execution
The second check runs `bazel test --build_tests_only --test_tag_filters=-requires-kvm //...`. This command executes all unit and integration tests across the workspace, excluding those tagged with `requires-kvm`. KVM-dependent tests, such as firecracker end-to-end scenarios, are typically too slow or resource-intensive for local pre-commit checks. Filtering them out aligns the local developer experience with the faster CI pipeline checks while still catching regressions in core logic.

## Key Interfaces
- `setup-pre-commit-hook.sh`: Entry point script that creates and configures the Git hook.

## Dependencies
- Git (for hook management)
- Bazel (for build and test execution)
- Gazelle (Bazel rule generator)

## Subcomponents
- `.git/hooks/pre-commit`: Generated hook script (created at runtime).