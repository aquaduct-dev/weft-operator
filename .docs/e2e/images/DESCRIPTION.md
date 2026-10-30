<!-- docdaemon ts:2026-10-30T03:01:25Z hash:83a1474a00af90b5 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory defines the preloaded container images and static node assets required for airgapped e2e microVM testing.

## Overview
The `e2e/images` package provides a self-contained set of binaries and images injected into every e2e microVM before execution. This ensures tests run against a consistent, offline environment without network dependencies. It serves as the foundation for stable, reproducible integration tests in isolated environments.

## Deep Dive
### Image Definitions
Images are exposed via two Bazel targets: `<name>_load` for importing the image using `oci_load`, and `<name>_tar` exposing the tarball via the `tarball` output group. These are consumed by `firecracker_k3s_test`'s `extra_images`. The collection includes core infrastructure (`pause`, `coredns`, `busybox`, `registry`), K3s components (`local_path_provisioner`, VPA modules), and Weft Operator/Agent artifacts built from source or external sources.

### Node Assets
Static files are copied into `bazel-out` to guarantee stability across Bazel invocations, preventing issues with vanishing external repo files. This includes the Mozilla CA bundle (`node_ca_bundle`) for system trust, Gateway API CRDs (both experimental and standard), and the Helm binary for chart-based scenarios.

## Key Interfaces
- `<name>_load`: OCI load target for importing specific container images.
- `<name>_tar`: Filegroup exposing the image tarball for test injection.
- `node_ca_bundle`: Static CA bundle file for trust configuration.
- `gateway_api_crds`: Static CRD manifests for Kubernetes API extensions.
- `helm`: Static Helm binary for chart management.

## Dependencies
- rules_firecracker (pinned via git_override)
- oci_image rules
- k3s distribution artifacts
- Weft operator source code
- Mozilla CA bundle source

## Subcomponents
- BUILD.bazel