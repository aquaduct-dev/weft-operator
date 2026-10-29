<!-- docdaemon ts:2026-10-29T20:36:23Z hash:008e3887ef335063 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file bootstraps an isolated Kubernetes control plane to run integration tests for the WeftTunnel controller.

The `TestControllers` entry point initializes Ginkgo and Gomega. The `BeforeSuite` block sets up a real etcd and API server via `envtest.Environment`, loading Custom Resource Definitions (CRDs) from both the Helm chart source and generated paths. It configures a `client.Client` and starts a `ctrl.Manager` with metrics exposed on port 8083, allowing the reconciler logic to execute against a simulated cluster state. The `AfterSuite` ensures cleanup of the manager context and test environment.