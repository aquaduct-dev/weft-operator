<!-- docdaemon ts:2026-10-29T20:36:18Z hash:829e9a4f0b995668 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `wefttunnel` controller package, including the main library and its associated integration tests.

### Build Targets

**`go_library(name="wefttunnel")`**
Compiles `reconciler.go` into the `aquaduct.dev/weft-operator/internal/controller/wefttunnel` package. It depends on Kubernetes API types (`apps/v1`, `core/v1`) and various `controller-runtime` subpackages for reconciliation logic, client interaction, and metrics.

**`go_test(name="wefttunnel_test")`**
Runs the test suite located in `*_test.go` files. The tests use `envtest` to spin up a local control plane, requiring specific data assets like `etcd`, `kube-apiserver`, and `kubectl`. The environment variable `KUBEBUILDER_ASSETS` is configured to point to these binaries. Dependencies include Ginkgo/Gomega for testing, `client-go` for scheme registration, and the full `controller-runtime` stack for logging and metrics server setup.

**`filegroup(name="srcs")`**
A public visibility group exposing all `.go` files in the directory, useful for tools that need to scan the entire package source.