<!-- docdaemon ts:2026-10-29T20:35:40Z hash:8cf276b9c11814d7 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file configures the Gazelle tool to generate Bazel build rules for the Go module and exports the dependency manifests.

### Purpose
The `BUILD.bazel` file serves as the root build configuration for the project, primarily delegating build rule generation to Gazelle. It ensures that the Go module dependencies (`go.mod`, `go.sum`) are accessible to other targets within the repository.

### Key Configurations

*   **Gazelle Integration**: The file loads Gazelle definitions from `@gazelle//:def.bzl`. A directive `gazelle:prefix aquaduct.dev/weft-operator` sets the import path prefix for generated Go rules, ensuring consistent package naming across the workspace.
*   **File Exports**: The `exports_files` function exposes `go.mod` and `go.sum` at the repository root. This allows external tools or internal targets to reference these files directly without needing to know their specific location relative to a sub-package.