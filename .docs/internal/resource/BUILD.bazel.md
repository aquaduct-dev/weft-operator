<!-- docdaemon ts:2026-10-29T20:36:41Z hash:c1eb88044fafddba model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rules for the `resource` Go package, exposing both the compiled library and its source files.

The `go_library` rule compiles `resource.go` into a Go library target named `resource`. It sets the import path to `aquaduct.dev/weft-operator/internal/resource`, ensuring correct module resolution within the project. The target is marked as public, allowing other packages in the repository to depend on it. This structure supports the resource CRUD framework introduced in recent development cycles.

Additionally, a `filegroup` named `srcs` is defined to expose all `.go` files in the directory via `glob`. This target is also public, facilitating tools or tests that require access to the raw source files rather than just the compiled output.