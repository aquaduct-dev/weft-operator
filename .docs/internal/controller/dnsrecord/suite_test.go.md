<!-- docdaemon ts:2026-10-29T20:36:09Z hash:5c1c3d48695b0b67 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file sets up the integration test environment for the DNSRecord controller using Ginkgo and envtest.

The `BeforeSuite` block initializes a local Kubernetes control plane via `envtest.Environment`, loading Custom Resource Definitions (CRDs) from the generated directory specified by the `TEST_SRCDIR` environment variable. It configures a `controller-runtime` manager with metrics disabled (`BindAddress: "0"`) to avoid port conflicts during testing, starts the manager in a goroutine, and exposes the resulting `rest.Config` and `client.Client` for use in individual tests. The `AfterSuite` ensures cleanup by stopping the test environment.