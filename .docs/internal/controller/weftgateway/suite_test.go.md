<!-- docdaemon ts:2026-10-29T20:36:12Z hash:316c037a39c79f64 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file sets up the Ginkgo test suite for the WeftGateway controller, initializing a local Kubernetes environment and manager.

The `TestControllers` entry point registers Ginkgo/Gomega handlers. The `BeforeSuite` block bootstraps an `envtest.Environment`, loading CRDs from `_main/tools/envtest` and `_main/api/v1alpha1/crds`. It configures a `rest.Config` and creates a `client.Client` using a shared `runtime.Scheme` that includes Weft API types and Gateway API v1/v1alpha2 resources. A `ctrl.Manager` is started in a goroutine to enable realistic reconciler testing against the fake cluster. The `AfterSuite` ensures cleanup by stopping the environment.