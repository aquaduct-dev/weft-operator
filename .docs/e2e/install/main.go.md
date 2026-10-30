<!-- docdaemon ts:2026-10-30T02:59:32Z hash:40a4b34f90a91e1e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file is the entry point for the guest-side e2e installation tests, orchestrating scenarios like CRD validation and Helm chart deployment within a microVM.

The `main` function initializes the test runner by registering specific scenario handlers (`crdsSteps` and `chartSteps`) via `e2ekit.Scenarios`. It delegates execution to `e2ekit.Main`, which manages the lifecycle of these tests against the k3s environment running in the firecracker microVM. The exit code from the test run is propagated directly to the OS. This structure allows distinct test logic (defined in `crds_scenario.go` and `chart_scenario.go`) to be driven by a single binary based on the `--scenario` flag.