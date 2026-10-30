<!-- docdaemon ts:2026-10-30T02:59:53Z hash:e17e3fea6e2c505a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `weftserver` controller package, including the main library and its associated unit and integration tests.

The `go_library` target compiles `node_prober.go` and `reconciler.go`, exposing them publicly for downstream consumers. It links against Kubernetes API types (`core/v1`, `apps/v1`, `batch/v1`) and the `controller-runtime` framework to manage WeftServer resources. Dependencies include internal packages like `api/v1alpha1` and `internal/resource`, as well as external clients for communicating with Weft servers.

Two test targets are defined:
- `weftserver_test`: An integration test suite using `envtest` to spin up a real Kubernetes control plane. It requires specific data assets (etcd, kube-apiserver, kubectl) and sets the `KUBEBUILDER_ASSETS` environment variable. This target uses Ginkgo/Gomega for assertions and includes tests for client caching, node probing, IP handling, and reconciliation logic.
- `weftserver_internal_test`: A faster, isolated unit test target that does not require external binaries or a full control plane. It focuses on internal logic such as client cache behavior and node prober mechanics using fake clients.

Historical context indicates recent optimizations to prevent connection leaks in the reconciler by caching Weft clients per server, and fixes to stop status write-loops from starving the manager. The build structure supports these components by separating heavy integration tests from lightweight internal checks.