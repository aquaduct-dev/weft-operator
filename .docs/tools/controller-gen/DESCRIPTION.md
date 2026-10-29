<!-- docdaemon ts:2026-10-29T20:38:54Z hash:a7c23719803eadf6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory provides Bazel integration for the `controller-gen` tool, enabling reproducible generation of Kubernetes manifests and Go code.

## Overview
The `tools/controller-gen` directory bridges the gap between the external `controller-tools` repository and the local Bazel build system. It exposes a public alias for the binary and defines custom rules to handle the specific output requirements of controller-gen, such as deep copy generators, RBAC roles, CRDs, and webhooks. This setup ensures that generated artifacts are managed strictly within Bazel’s dependency graph, avoiding manual file management and ensuring consistent build environments.

## Deep Dive
### Rule Implementation
The `defs.bzl` file introduces `controller_gen_rule`, which wraps the `controller-gen` binary in a controlled execution environment. To guarantee reproducibility, it isolates cache and module data in temporary directories. The rule executes the generator and then maps its flexible output paths to Bazel’s strict `outs` or `dirs` declarations by locating files by name. This abstraction handles the mismatch between controller-gen’s dynamic output structure and Bazel’s static output expectations.

### Macro Abstractions
To reduce boilerplate, the `controller_gen` macro provides specialized sub-rules for common Kubernetes artifacts:
- **deepcopy**: Generates `zz_generated.deepcopy.go` with a null header.
- **rbac**: Produces `role.yaml` in the current directory.
- **crds**: Outputs Custom Resource Definition YAMLs into a dedicated `crds/` directory.
- **webhook**: Generates webhook configurations when specific outputs are requested.

The macro automatically injects necessary arguments for role naming and CRD structure, scanning packages relative to the source list while excluding the generator’s own package. Users can override path discovery via `override_paths` if standard resolution is insufficient.

### External Dependency Management
The `BUILD.bazel` file creates a public alias named `controller-gen`. This alias re-exports the `//cmd/controller-gen` target from the external `@io_k8s_sigs_controller_tools` dependency. By centralizing this reference, the project allows any internal package to depend on the tool without needing to know the full external path, simplifying consumption and maintenance.

## Key Interfaces
- `controller_gen_rule`: A Bazel rule that executes controller-gen and manages output mapping.
- `controller_gen`: A macro that simplifies usage by providing specialized targets for deepcopy, rbac, crds, and webhook generation.
- `controller-gen` (alias): A public Bazel alias pointing to the external controller-tools binary.

## Dependencies
- `@io_k8s_sigs_controller_tools`: The external repository containing the controller-gen binary.
- Standard Bazel Go toolchain rules for executing Go binaries.

## Subcomponents
- `BUILD.bazel`: Defines the public alias for the controller-gen tool.
- `defs.bzl`: Contains the `controller_gen_rule` implementation and `controller_gen` macro.