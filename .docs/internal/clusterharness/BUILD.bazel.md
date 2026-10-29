<!-- docdaemon ts:2026-10-29T20:36:02Z hash:2ee629d7cc36aba2 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `clusterharness` Go package and its associated tests.

The `go_library` target compiles `clusterharness.go` into a library named `clusterharness`. It is exposed to subpackages via `visibility`, allowing internal components to import it as `aquaduct.dev/weft-operator/internal/clusterharness`.

The `go_test` target builds `clusterharness_test.go` and embeds the main library, enabling unit tests to access internal implementation details. This setup supports the package's role in providing infrastructure for end-to-end testing, such as booting isolated environments (e.g., firecracker microVMs) to verify operator behavior like DNS reflection in airgapped scenarios.