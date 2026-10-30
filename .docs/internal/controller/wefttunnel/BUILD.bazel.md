<!-- docdaemon ts:2026-10-30T02:59:58Z hash:f143b591367fe49a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This Bazel build file defines the Go library and integration tests for the WeftTunnel controller, managing dependencies on Kubernetes APIs and the controller-runtime framework.

### Library Definition (`wefttunnel`)
The `go_library` target compiles `reconciler.go` into the package `aquaduct.dev/weft-operator/internal/controller/wefttunnel`. It exposes the library publicly to allow other packages to depend on it. The dependency list includes:
- **Core Kubernetes APIs**: `apps/v1` (Deployments) and `core/v1` (Pods, Services).
- **Controller Runtime**: Provides the reconciliation loop, client interfaces, and utility functions for resource management.
- **Internal Packages**: Depends on `//api/v1alpha1` for custom resource definitions and `//internal/resource` for shared CRUD logic.

### Test Suite (`wefttunnel_test`)
The `go_test` target runs integration tests using Ginkgo and Gomega. It requires a local Kubernetes control plane via `envtest`, configured through the `data` attribute which pulls in necessary binaries like `etcd`, `kube-apiserver`, and `kubectl`.
- **Environment Setup**: The `KUBEBUILDER_ASSETS` environment variable is set to point to the extracted test binaries, ensuring the tests can spin up a fake API server.
- **Test Coverage**: Includes tests for idempotency, status updates, and general reconciliation logic.
- **Dependencies**: Extends the library deps with `controller-runtime/pkg/envtest` for cluster setup and `client-go` for scheme registration.

### Build History Context
Recent changes indicate a shift toward stricter isolation and correctness:
- Tests were fully isolated to prevent cross-test pollution.
- Deployment reconciliation was fixed to avoid unnecessary updates by preserving existing container fields, preventing infinite update loops caused by API defaults.
- Gateway API CRDs are no longer bundled in the chart but are required externally, with discovery checks added to fail fast if missing.