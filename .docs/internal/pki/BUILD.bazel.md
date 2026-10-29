<!-- docdaemon ts:2026-10-29T20:36:28Z hash:83bcbfc2b77fd6e1 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the internal PKI package, exposing source files and the compiled library to other parts of the repository.

## Build Targets

The `filegroup` named `srcs` aggregates all Go source files in the directory using `glob`. It is marked as public visibility, allowing any target in the repository to depend on the raw source files if needed.

The `go_library` named `pki` compiles `pki.go` into a Go library. Its import path is set to `aquaduct.dev/weft-operator/internal/pki`, ensuring correct module resolution. Visibility is restricted to `//:__subpackages__`, meaning only subpackages within this directory can directly depend on it, enforcing encapsulation of the internal implementation.

The `go_test` named `pki_test` builds the test suite from `pki_test.go`. It embeds the `:pki` library, granting the tests access to the package's internal types and functions for comprehensive unit testing.