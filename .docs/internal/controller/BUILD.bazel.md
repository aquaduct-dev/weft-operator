<!-- docdaemon ts:2026-10-29T20:36:04Z hash:6b9c6a56213cc5bf model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rule for the controller integration tests.

The `go_test` target `controller_test` executes the test suite defined in `suite_test.go`. It configures the environment by loading necessary dependencies and data files required for running tests against a local Kubernetes cluster using `envtest`.

### Dependencies
- **Source**: `suite_test.go` contains the actual test logic.
- **Data**: Includes generated CRDs from `//api/v1alpha1` and Gateway API CRDs from `//tools/envtest`. These are injected into the test environment to simulate the presence of custom resources during execution.
- **Libraries**: Links against Ginkgo/Gomega for testing, client-go for Kubernetes interaction, and controller-runtime packages for client setup, logging, and envtest management.

### Context
Historically, this target was updated to fully isolate tests (commit c8e2c3a9a2) and fix various test failures (commit e6a258b1d1). The inclusion of Gateway API CRDs reflects a shift away from bundling them in Helm charts, requiring the operator to ensure these CRDs are available in the test environment explicitly.