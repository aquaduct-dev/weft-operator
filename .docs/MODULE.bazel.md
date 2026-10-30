<!-- docdaemon ts:2026-10-30T02:59:21Z hash:b28dad26a11f5507 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel module configuration, declaring dependencies and pinned external resources for building and testing the Weft Operator.

It establishes `weft_operator` as a Bazel module, importing core tooling via `bazel_dep`: `rules_oci` for container management, `rules_go` and `gazelle` for Go build rules, `rules_helm` for Helm chart integration, and `rules_shell` for shell utilities. The Go SDK is explicitly downloaded (v1.25.0) rather than using the system default, ensuring reproducible builds across environments.

Direct Go dependencies are resolved from `//:go.mod` and registered via `use_repo`, including Kubernetes client libraries (`k8s/client-go`, `controller-runtime`) and utility packages like `spf13/cobra`. This ensures all transitive Go dependencies are available to the build graph without requiring a `WORKSPACE` file.

For e2e testing, the module configures an airgapped environment using `rules_firecracker`. It pins specific versions of `aquaduct_weft` and `rules_firecracker` via `git_override` to ensure tests run against exact code states. External artifacts required by the test VMs—such as k3s, CoreDNS, local-path provisioner, VPA components, and Mozilla CA bundles—are pre-downloaded using `http_file` and `oci.pull` with strict SHA256 checksums. This prevents network dependency during test execution and guarantees deterministic behavior. Gateway API CRDs are also fetched to satisfy operator startup requirements.