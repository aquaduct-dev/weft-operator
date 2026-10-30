<!-- docdaemon ts:2026-10-30T02:59:37Z hash:c8beb895cac4326d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the trust-injection webhook end-to-end test binary and its associated container image.

The `trust_lib` go_library aggregates the test logic (`main.go`, `netns.go`, `webhook.go`) and depends on Kubernetes API types, internal PKI utilities, and e2e kit helpers. It is marked private as it is an implementation detail of the test suite.

The `truste2e` go_binary compiles this library into a static, pure Go executable suitable for running in the airgapped firecracker microVM environment, which lacks libc. This binary drives the actual test steps against the k3s cluster.

The `alpine` target wraps `truste2e` into a container image using `e2e_binary_image`. This Alpine-based image provides a real system trust store at standard bundle paths, allowing the test to verify that injected volumes are correctly mounted and readable by the application containers.

Finally, `webhook_test` orchestrates the execution via `weft_e2e_test`. It injects the operator manifest (with the default reflector off) and namespace definitions into the test environment. The test scenario validates webhook configuration fields, injection behavior with and without a reflector, opt-out mechanisms, failure policies, and idempotency across restarts.