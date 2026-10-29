<!-- docdaemon ts:2026-10-29T20:35:42Z hash:4bd690ad31175e23 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines critical agent instructions for repository maintenance, build configuration, and coding standards.

It enforces strict Bazel usage rules, specifically prohibiting `WORKSPACE` files and mandating the use of `rules_go`. It outlines a rigorous development workflow requiring `go_diagnostics` validation before testing and `go_package_api` for library integration. The document emphasizes security by forbidding sensitive data in logs and mandates user confirmation for all git commits. Finally, it provides specific environment variables for running `envtest`-based tests.