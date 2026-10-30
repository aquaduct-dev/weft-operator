<!-- docdaemon ts:2026-10-30T02:59:21Z hash:d3db178d6c0d8b9f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file exposes the e2e test definitions for external consumption by other Bazel targets.

The `BUILD.bazel` file in the `e2e` directory serves as a public interface for the end-to-end testing infrastructure. It uses `exports_files(["defs.bzl"])` to make the `defs.bzl` macro file available to packages outside this directory, allowing other modules to import and utilize the shared e2e testing utilities without exposing internal implementation details.

This structure supports the modularization of the e2e suite, specifically enabling the separation of concerns between the core test logic (in `defs.bzl`) and its consumers. The current setup reflects an evolution where shared foundations, such as the guest harness (`e2ekit`) and preloaded images (`images`), are made accessible across different test scenarios. Notably, this configuration accommodates the migration from local firecracker tooling to `rules_firecracker`, ensuring that e2e tests can leverage pinned dependencies via `git_override` while maintaining a clean public API for test definition.