<!-- docdaemon ts:2026-10-29T20:36:24Z hash:72834256602475f3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rules for the `nodeagent` Go library and its associated unit tests.

The `go_library` target compiles `agent.go`, `certsd.go`, and `hostsfile.go` into a package named `nodeagent`. It exposes the library to subpackages via visibility settings and depends on logging infrastructure from `logr` and `controller-runtime`. The inclusion of `certsd.go` and `hostsfile.go` alongside the main agent logic suggests this module handles node-specific configuration, such as certificate management and local DNS resolution (e.g., reflecting public hostnames in-cluster to optimize traffic routing).

The `go_test` target builds `agent_test.go`, embedding the main library for testing purposes. It also links against the `controller-runtime` zap logger dependency to ensure test execution uses the correct logging setup consistent with the production code.