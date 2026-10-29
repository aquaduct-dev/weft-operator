<!-- docdaemon ts:2026-10-29T20:35:40Z hash:001902d6a8e5e17d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `v1alpha1` API types, managing source collection, code generation, and library compilation.

The `filegroup` named `api_types` aggregates all Go source files in the directory, explicitly excluding the auto-generated deepcopy functions to prevent circular dependencies during generation. This group serves as the input set for the `controller_gen` rule.

The `controller_gen` target invokes the controller-gen tool to generate CRD manifests and webhook configurations. It processes the `api_types` sources along with internal controller and PKI packages to ensure generated code is consistent across the operator's components. The output is marked public so downstream consumers can access the generated artifacts.

The `go_library` target named `v1alpha1` compiles the final API package. It includes the manual type definitions (e.g., `aquaducttaas_types.go`, `dnsrecord_types.go`) and the generated deepcopy methods (`:generated.deepcopy`). It depends on standard Kubernetes client-go libraries (`core/v1`, `meta/v1`, `runtime`) and the controller-runtime scheme package. This library exposes the v1alpha1 API objects for use by controllers and webhooks throughout the project.