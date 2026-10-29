<!-- docdaemon ts:2026-10-29T20:36:45Z hash:7001b3aaa1d0aced model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the trust injector webhook, which manages PKI certificates and handles admission requests.

The `go_library` target compiles `handler.go` and `setup.go`, exposing the core logic for injecting trust into pods. It depends on internal PKI and trust packages to manage certificate lifecycles, alongside Kubernetes API types for handling admission reviews.

The `go_test` target provides unit tests for the injector, embedding the library and utilizing a fake client from `controller-runtime` to simulate cluster interactions without a live environment.

A recent architectural change introduced `WeftLocalDNSReflector` support to optimize intra-cluster traffic. This refactor ensures that public hostnames are resolved locally via WireGuard tunnels rather than hairpinning through bastion hosts, reducing latency for bulk operations like image registry pulls.