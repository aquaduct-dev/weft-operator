<!-- docdaemon ts:2026-10-30T02:59:21Z hash:c6c4a303227f901b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for compiling the Weft Operator binary, packaging it into multi-architecture OCI images, and running unit tests.

### Build Targets

*   **`cmd_lib`**: A `go_library` target that compiles `main.go`. It is marked private as it serves only as an embeddable dependency for binaries and tests. It depends on internal controllers (e.g., `aquaducttaas`, `dnsrecord`) and Kubernetes client libraries.
*   **`cmd_raw`**: A `go_binary` that embeds `cmd_lib`. The `pure = "on"` flag ensures a static binary suitable for minimal base images like Alpine.
*   **`cmd` / `cmd_arm64`**: `go_cross_binary` targets that compile `cmd_raw` for `linux_amd64` and `linux_arm64` respectively. These produce the actual executable artifacts.
*   **`layer_amd64` / `layer_arm64`**: `pkg_tar` targets that package the cross-compiled binaries into tarballs for inclusion in container images.

### Container Images

*   **`weft_operator_image_amd64` / `_arm64`**: `oci_image` targets that create single-architecture images using Alpine bases. They inject the respective binary layers and set the entrypoint to `/cmd` or `/cmd_arm64`.
    *   The AMD64 image has visibility restricted to `//e2e:__subpackages__` because e2e tests preload this specific image into microVMs.
*   **`weft_operator_image`**: An `oci_image_index` that combines the AMD64 and ARM64 images into a multi-arch manifest.
*   **`weft_operator_image_push`**: Pushes the multi-arch index to `ghcr.io/aquaduct-dev/weft-operator`.
*   **`weft_operator_image_load`**: Loads the AMD64 image into the local Docker daemon with the tag `ghcr.io/aquaduct-dev/weft-operator:latest` for local development convenience.

### Testing

*   **`cmd_test`**: A `go_test` target for `main_test.go`. It embeds `cmd_lib` and adds fake discovery/testing dependencies from the Kubernetes client-go library.