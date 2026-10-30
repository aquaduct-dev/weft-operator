<!-- docdaemon ts:2026-10-30T02:59:49Z hash:fee78a22b949c1b8 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `weftgateway` controller package, including the main library and its associated test suite.

### Build Targets

**Library (`weftgateway`)**
The core Go library implements the Gateway API controller logic. It depends on:
- `controller-runtime` packages for reconciliation, client interaction, and logging.
- `gateway-api` APIs (v1 and v1alpha2) to process Gateway resources.
- Internal project modules (`api/v1alpha1`, `internal/resource`) for custom types and resource management.
- Kubernetes apimachinery utilities for error handling and runtime objects.

**Tests (`weftgateway_test`)**
The test target runs unit and integration tests using Ginkgo/Gomega. It embeds the main library and includes:
- **Data Dependencies**: Local copies of CRDs (`generated.crds`, `gateway-api-crds.yaml`) and environment binaries (`etcd`, `kube-apiserver`, `kubectl`) required for `envtest`.
- **Environment Configuration**: Sets `KUBEBUILDER_ASSETS` to point to the local etcd binary path, enabling isolated Kubernetes API server testing.
- **Test Dependencies**: Includes `controller-runtime/pkg/envtest`, `fake` client, and metrics server support for comprehensive testing infrastructure.

**Source Grouping (`srcs`)**
A public `filegroup` aggregates all `.go` files in the directory, simplifying visibility rules for other packages that may need access to the source tree without depending on the compiled library.