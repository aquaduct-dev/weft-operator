<!-- docdaemon ts:2026-10-29T20:35:40Z hash:b13299d7dad4ae7e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel module configuration, external dependencies, and pinned artifacts for the `weft_operator` project.

It registers core toolchains (`rules_go`, `gazelle`) and container rules (`rules_oci`, `rules_helm`). Go SDK 1.25.0 is downloaded explicitly, and Go module dependencies are resolved via Gazelle extensions, with explicit repo registrations for Kubernetes ecosystem libraries (e.g., `controller-runtime`, `client-go`).

The module also manages e2e test infrastructure by pinning specific versions of tools and images to ensure airgapped, reproducible microVM testing:
- **Firecracker VMs**: Uses a pre-built Linux kernel source, static Busybox, and k3s binary.
- **Cluster Components**: Pins CoreDNS and Registry images used by the reflector tests.
- **Dependencies**: Includes Gateway API CRDs and Mozilla CA bundle for trust store simulation.
- **Local Dev**: Overrides the `aquaduct_weft` dependency to point to a specific git commit, allowing local development against the exact operator version being tested.