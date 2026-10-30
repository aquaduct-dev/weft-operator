<!-- docdaemon ts:2026-10-30T02:59:37Z hash:b273484b3f0f39c6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `reflector` e2e test binary, acting as a multi-mode guest agent for WeftLocalDNSReflector integration tests.

The binary supports four execution modes determined by command-line arguments:

*   **Default Scenario**: Runs on a firecracker microVM alongside k3s. It parses a `--scenario` flag to load YAML definitions, initializes a Kubernetes client, and executes specific test steps (either `defaultReflectorSteps` or generic `reflectorSteps`). Results are written to a JSON report via `clusterharness`. This mode handles the core validation of DNS reflection and trust injection.
*   **Client**: Simulates a real workload inside a pod. It resolves reflected hostnames, performs TLS-verified HTTPS requests using only the system trust store, and pushes an image.
*   **Sleep**: A minimal entrypoint used by the pushed image to keep the container running after being pulled through the reflector.
*   **Fetch**: Performs a plain HTTP GET request against a reflected route.

Additionally, if the `--scenario` argument matches predefined extra scenarios (like `reflector-claims`), the binary delegates to `e2ekit.Main` for handling those specific test flows. The `scenarioArg` helper extracts the scenario name from raw arguments before standard flag parsing to support these alternative modes.