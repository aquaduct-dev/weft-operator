<!-- docdaemon ts:2026-10-30T02:59:52Z hash:e5c64f29e978e1a6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rules for the `internal/resource` Go package, exposing its library and test targets.

The `go_library` target compiles `image.go` and `resource.go` into a shared library at import path `aquaduct.dev/weft-operator/internal/resource`. It depends on the Kubernetes API core types via `@io_k8s_api//core/v1:core`, indicating these resources interact with standard Kubernetes objects. The target is marked public to allow other packages in the repository to depend on it.

A `filegroup` named `srcs` exposes all `.go` files in the directory for tooling or documentation purposes.

The `go_test` target runs `image_test.go` against the main library using the `embed` attribute, ensuring tests have access to the production code without separate compilation steps.