<!-- docdaemon ts:2026-10-30T02:59:34Z hash:583dd85805a0d5c7 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for end-to-end tests that verify WeftLocalDNSReflector behavior in an isolated k3s environment.

It compiles a static Go binary (`reflectore2e`) containing test logic and client utilities, then packages it into a preloaded image (`client`). The `weft_e2e_test` rules orchestrate scenarios by injecting Helm-rendered operator manifests (with optional default reflector toggling) and tarballs into a firecracker microVM rootfs. Specific test targets cover basic traffic reflection, complex routing claims, and lifecycle events like certificate reissuance and proxy failures.