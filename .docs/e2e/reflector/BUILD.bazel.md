<!-- docdaemon ts:2026-10-29T20:35:54Z hash:c126eeac2572fbbc model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for the WeftLocalDNSReflector end-to-end tests, which validate in-cluster traffic routing using a firecracker microVM.

The `reflector_lib` Go library contains the test logic, including the client binary and scenario drivers. It depends on Kubernetes API types and internal cluster harness utilities to orchestrate the test environment.

Two primary test targets are defined:
- `reflector_test`: Verifies that a manually configured reflector correctly routes traffic for a specific hostname (`registry.weft-e2e.test`) within an airgapped k3s VM.
- `default_reflector_test`: Validates the operator's ability to automatically create and manage a `cluster-default` reflector when enabled via chart values.

Both tests utilize `firecracker_k3s_test` to boot a single-node k3s cluster inside a microVM. The setup preloads necessary container images (pause, coredns, registry, operator, weft, and e2e client) into the VM’s local store since it has no network access. Essential files, such as Gateway API CRDs and the system CA bundle, are injected into the root filesystem to ensure proper certificate verification and API availability.

The build process generates OCI images from static Go binaries and Helm templates rendered with specific E2E values. This ensures the test environment mirrors production configurations while remaining isolated and reproducible.