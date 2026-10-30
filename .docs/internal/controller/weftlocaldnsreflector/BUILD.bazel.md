<!-- docdaemon ts:2026-10-30T02:59:49Z hash:9f21ffcec5c73f8e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `weftlocaldnsreflector` controller package.

The `go_library` target compiles the core logic for the `WeftLocalDNSReflector`, a cluster-scoped controller that keeps public hostnames resolvable inside the cluster to avoid hairpinning traffic through bastions. It depends on Kubernetes API types, controller-runtime, and internal packages for PKI and trust management.

The `go_test` target runs unit tests using Ginkgo/Gomega and envtest. It embeds the library and configures the test environment with local etcd and kube-apiserver binaries to simulate a Kubernetes control plane.