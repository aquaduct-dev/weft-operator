<!-- docdaemon ts:2026-10-29T20:35:59Z hash:1f5e11b6deb88e2d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the WeftServer controller, including the main library and isolated test suites.

The `go_library` target compiles `node_prober.go` and `reconciler.go` into the `weftserver` package. It depends on Kubernetes API types (`core/v1`, `apps/v1`, `batch/v1`) and the controller-runtime framework. The `filegroup` exposes all Go sources in this directory for public visibility, facilitating external tooling or documentation generation.

Two test targets are defined:
- `weftserver_test`: An integration test suite using `envtest`. It embeds the library and requires runtime assets like `etcd`, `kube-apiserver`, and `kubectl` via the `data` attribute. The `KUBEBUILDER_ASSETS` environment variable is configured to locate these binaries. Tests use Ginkgo/Gomega and verify behavior against a fake Kubernetes control plane.
- `weftserver_internal_test`: A unit test suite that runs without external dependencies. It embeds the library and uses `fake.NewClientBuilder` from controller-runtime to mock client interactions, ensuring fast, isolated execution.

Historical context indicates recent optimizations to prevent connection leaks in the Weft client cache and to stop status update loops from starving the controller manager.