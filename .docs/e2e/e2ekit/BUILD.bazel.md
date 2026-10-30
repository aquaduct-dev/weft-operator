<!-- docdaemon ts:2026-10-30T02:59:28Z hash:bf7350918509196a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the `e2ekit` Go package, which provides shared infrastructure and utilities for end-to-end testing.

The `go_library` target compiles `e2ekit.go` into a reusable library. It exposes the package publicly to allow other test suites to import it. The library depends on Kubernetes API types (`admissionregistration`, `apps`, `core`, `meta`) and client-go libraries, enabling interaction with cluster resources during tests. It also links against `controller-runtime` and `gateway-api` clients.

The `go_test` target builds `e2ekit_test.go`, embedding the main library to access its internal helpers. This separation ensures that test code does not pollute the production library binary while allowing comprehensive coverage of the e2e framework logic.