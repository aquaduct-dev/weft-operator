<!-- docdaemon ts:2026-10-30T02:59:30Z hash:133e3528336ad15b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the preloaded container images and static node assets required for airgapped e2e microVM testing.

### Purpose
The `e2e/images` package provides a self-contained set of binaries and images that are injected into every e2e microVM before execution. This ensures tests run against a consistent, offline environment without network dependencies.

### Image Definitions
Images are exposed as two targets:
- `<name>_load`: An `oci_load` target for importing the image.
- `<name>_tar`: A `filegroup` exposing the tarball via the `tarball` output group, used by `firecracker_k3s_test`'s `extra_images`.

The list includes:
- **Core Infrastructure**: `pause`, `coredns`, `busybox`, and `registry`.
- **K3s Components**: `local_path_provisioner` and VPA components (recommender, updater, admission controller).
- **Weft Operator & Agent**: Built from source (`weft_image`) or external artifacts (`operator`).

### Node Files
Static files are copied into `bazel-out` to ensure stability across Bazel invocations, as external repo files may vanish. These include:
- `node_ca_bundle`: The Mozilla CA bundle for system trust.
- `gateway_api_crds`: Both experimental and standard Gateway API CRDs.
- `helm`: The Helm binary for chart-based scenarios.