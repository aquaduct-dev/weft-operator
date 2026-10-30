<!-- docdaemon ts:2026-10-30T03:00:00Z hash:c9488818e308824c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the trust injection webhook package, including the main library and its associated tests.

The `trustinjector` `go_library` compiles `handler.go` and `setup.go`, exposing the logic for mutating admission requests to inject trust bundles into pods. It depends on internal PKI and trust packages for certificate management, alongside Kubernetes API types and controller-runtime utilities for webhook registration and client interactions.

The `trustinjector_test` `go_test` target embeds the library to run unit tests against the webhook logic. It utilizes a fake Kubernetes client to simulate cluster state, ensuring that mutations are applied correctly based on annotations and namespace labels. Recent changes indicate this setup supports e2e testing for k3s environments, verifying that the webhook respects opt-out mechanisms and correctly handles CA bundle mounting in init and app containers without interfering with existing volume mounts.