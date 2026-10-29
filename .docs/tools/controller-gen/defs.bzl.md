<!-- docdaemon ts:2026-10-29T20:36:22Z hash:ac4c7fcf51e14c3a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines a Bazel rule and helper macro to generate Kubernetes manifests using `controller-gen`.

The `controller_gen_rule` executes the `controller-gen` binary within a controlled Go environment. It sets up temporary directories for cache and module data, ensuring reproducible builds by isolating state. The shell script runs the tool, then locates generated files by name and copies them into declared outputs (`outs`) or directories (`dirs`). This approach handles the mismatch between controller-gen's flexible output paths and Bazel's strict output declarations.

The `controller_gen` macro simplifies usage by creating specific sub-rules for common artifacts:
- **deepcopy**: Generates `zz_generated.deepcopy.go` with a null header file.
- **rbac**: Generates `role.yaml` in the current directory.
- **crds**: Generates CRD YAMLs into a `crds/` directory.
- **webhook**: Generates webhook configurations if `webhook_outs` are provided.

By default, it scans all packages relative to the source list, excluding the generator's own package. Users can override this path discovery via `override_paths`. The macro automatically injects necessary arguments for RBAC role naming and CRD structure, reducing boilerplate in BUILD files.