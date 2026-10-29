<!-- docdaemon ts:2026-10-29T20:36:04Z hash:649b51d59d70ff5c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `aquaducttaas` controller package, which mirrors external bastion resources from the aquaduct.dev API into the cluster.

The `go_library` target builds the core logic in `client.go`, `domain.go`, and `reconciler.go`. It depends on `controller-runtime` for reconciliation loops and standard Kubernetes APIs for object management. The library exposes a public visibility to allow other packages to import the controller logic.

The `filegroup` target aggregates all Go sources in the directory, providing a convenient way to reference the entire source set if needed by other rules.

The `go_test` target runs unit and integration tests using Ginkgo and Gomega. It includes test sources (`client_test.go`, `reconciler_test.go`, `suite_test.go`) and declares data dependencies for the envtest environment, specifically requiring etcd, the kube-apiserver, kubectl, and Gateway API CRDs. The `KUBEBUILDER_ASSETS` environment variable is configured to point to the local toolchain assets, enabling isolated testing against a real Kubernetes control plane without an external cluster.