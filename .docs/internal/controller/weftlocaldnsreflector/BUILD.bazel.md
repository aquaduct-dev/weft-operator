<!-- docdaemon ts:2026-10-29T20:36:13Z hash:61dc35377c7d482a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `weftlocaldnsreflector` controller package.

The `go_library` target compiles the core logic for managing `WeftLocalDNSReflector` custom resources. It integrates with Kubernetes APIs (`core/v1`, `apps/v1`) and the controller-runtime framework to reconcile state. The library depends on internal packages for PKI management (`//internal/pki`) and trust configuration (`//internal/trust`), reflecting its role in handling TLS certificates and hostname routing.

The `go_test` target runs unit and integration tests using Ginkgo/Gomega. It embeds the main library and configures an `envtest` environment (etcd, kube-apiserver) to simulate a Kubernetes cluster during testing. This setup allows verification of the reconciler's behavior against real API objects without requiring a live cluster.