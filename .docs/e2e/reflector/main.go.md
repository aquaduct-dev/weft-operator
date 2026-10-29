<!-- docdaemon ts:2026-10-29T20:36:00Z hash:7ab4e86ca5c25976 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `reflector` binary, which serves as the guest-side orchestrator for WeftLocalDNSReflector end-to-end tests.

The binary operates in three distinct modes determined by its first argument:

*   **Scenario (default):** Runs on a Firecracker microVM alongside k3s. It loads a scenario YAML via `--scenario`, initializes a Kubernetes client using `--kubeconfig`, and executes test steps defined in `scenario.go` or `default_scenario.go`. Results are written to a JSON report at `--report-json`. The default reflector logic (`default-reflector`) is optimized to keep traffic in-cluster using the `allHostnames` mode, avoiding per-cluster setup overhead.
*   **Client:** Simulates a real workload inside a pod. It resolves reflected hostnames, performs TLS-verified HTTPS requests using only the system trust store, and pushes an image.
*   **Sleep:** A lightweight entrypoint used by the pushed image to keep the container running after being pulled back through the reflector.

Key functions include `runScenario`, which manages the test lifecycle and timeout context, and `main`, which dispatches execution based on the command-line mode.