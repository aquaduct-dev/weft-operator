<!-- docdaemon ts:2026-10-29T20:36:18Z hash:5c256e33385e661c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file bootstraps an isolated Kubernetes control plane for integration testing the WeftServer controller.

The `BeforeSuite` block initializes an `envtest.Environment`, loading Custom Resource Definitions (CRDs) from both the Helm chart and generated API paths to ensure full schema availability. It starts a local `controller-runtime` manager, allowing reconcilers to execute against real CRD logic without a live cluster. The `AfterSuite` ensures cleanup of the environment and context. Global variables (`cfg`, `k8sClient`, `testScheme`) are shared across test suites to maintain state between setup and execution phases.