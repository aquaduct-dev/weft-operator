<!-- docdaemon ts:2026-10-29T20:35:53Z hash:098dc754a8475e3f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rules for compiling the Weft Operator binary and packaging it into multi-architecture OCI images.

It establishes `cmd_lib` as a private Go library containing the operator's main logic, depending on internal controllers (e.g., `weftgateway`, `dnsrecord`) and Kubernetes client libraries. The actual binaries are built via `go_cross_binary` for `linux_amd64` and `linux_arm64`, ensuring cross-compilation support.

These binaries are packaged into separate tar layers (`layer_amd64`, `layer_arm64`) and combined with Alpine Linux bases into architecture-specific OCI images. An `oci_image_index` merges these into a single manifest named `weft_operator_image`.

The target `weft_operator_image_push` publishes this image to `ghcr.io/aquaduct-dev/weft-operator`. For local development, `weft_operator_image_load` loads the AMD64 variant into the local Docker daemon under the tag `ghcr.io/aquaduct-dev/weft-operator:latest`.

Visibility is restricted where appropriate: `cmd_lib` is private to prevent external dependencies, while the final push rule is public. The e2e tests have visibility into the AMD64 image to preload it into their microVMs.