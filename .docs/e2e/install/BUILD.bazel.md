<!-- docdaemon ts:2026-10-30T02:59:32Z hash:a3278a82dc655ad3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build targets for end-to-end tests that validate operator installation, chart upgrades, and lifecycle management within isolated microVMs.

### Build Targets

*   **`install_lib`**: Aggregates Go source files (`crds_scenario.go`, `chart_scenario.go`, etc.) into a library shared by test binaries. It depends on Kubernetes API libraries and internal e2e utilities.
*   **`installe2e`**: A static, pure Go binary embedding `install_lib`. It is designed to run in the firecracker microVM environment which lacks libc.

### Test Scenarios

The file configures two primary test suites using the custom `weft_e2e_test` rule:

1.  **`crds_test`**: Validates Gateway API CRD presence and operator readiness.
    *   Uses `helm_template` to generate `operator_manifest` from the chart.
    *   Tests scenarios including missing CRDs (preflight failure), standard readiness, route logging, and lease/probe health checks.
    *   Ensures the operator survives cache sync timeouts without restarting.

2.  **`chart_test`**: Validates Helm chart installation, upgrade, and uninstallation.
    *   Generates an "old" chart version (`chart_old`) by stripping `spec.location` defaults to simulate schema changes during upgrades.
    *   Extracts old CRDs (`chart_old_crds`) to test ownership conflicts and adoption via `--force-conflicts`.
    *   Includes VPA stack images and Helm binary as extra execution dependencies.
    *   Verifies behaviors such as unowned CRD rejection, automatic VPA installation, upgrade conflict resolution, and finalizer handling during uninstall.