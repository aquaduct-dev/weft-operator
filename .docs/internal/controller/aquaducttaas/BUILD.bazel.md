<!-- docdaemon ts:2026-10-30T02:59:44Z hash:8871d5681f4ece62 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `aquaducttaas` controller package, including the main library and its associated test suite.

The `go_library` target compiles `client.go`, `domain.go`, and `reconciler.go`. The reconciler mirrors bastion resources from the external `aquaduct.dev` API into the cluster as `WeftServer` CRs, handling synchronization and lifecycle management. It depends on standard Kubernetes client-go libraries and the controller-runtime framework.

The `go_test` target builds the test suite using Ginkgo and Gomega. It configures an integration test environment via `envtest`, requiring specific data assets like etcd, kube-apiserver, and kubectl to spin up a local control plane. The tests verify the reconciler's behavior against the API definitions in `//api/v1alpha1`.