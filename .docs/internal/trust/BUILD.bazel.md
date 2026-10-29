<!-- docdaemon ts:2026-10-29T20:36:44Z hash:a042d004ee2fa320 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rules for the `trust` package, which manages certificate bundles and injection logic.

The `go_library` target compiles `bundle.go` and `inject.go` into the `aquaduct.dev/weft-operator/internal/trust` module. It depends on Kubernetes API types (`core/v1`, `meta/v1`, `types`) and controller-runtime utilities to interact with cluster resources. The inclusion of `//internal/pki` suggests this package handles public key infrastructure operations, likely related to managing trust anchors or certificates.

The `filegroup` exposes all Go sources in the directory for external visibility, facilitating testing or tooling that needs access to the raw source files.

The `go_test` target runs unit tests defined in `trust_test.go`. It embeds the main library and adds dependencies on `pki` and Kubernetes metadata APIs, indicating tests verify PKI-related behaviors and Kubernetes object interactions.

Recent changes introduced a `WeftLocalDNSReflector` to optimize internal traffic routing by keeping public hostname resolution within the cluster, reducing latency caused by hairpinning through bastion hosts and WireGuard tunnels. This optimization likely influenced how trust or DNS-related configurations are handled in the broader operator context, though this specific BUILD file primarily focuses on the compilation and testing structure of the trust management code.