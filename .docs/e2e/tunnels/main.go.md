<!-- docdaemon ts:2026-10-30T02:59:44Z hash:6ad1b463d16e9d1a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file serves as the entry point for the guest-side binary in tunnel end-to-end tests, orchestrating scenario execution and providing backend utilities.

The `main` package implements a single static binary with three operational modes determined by command-line arguments. It integrates with `e2ekit` to drive test scenarios within a firecracker microVM environment alongside k3s.

### Modes of Operation

*   **Default (Scenario)**: When no subcommand is provided, the binary initializes the e2e framework. It registers two specific scenarios:
    *   `tunnels`: Executes `tunnelsSteps`, validating internal WeftServers, Gateway controllers, tunnel Deployments, fan-out behavior, HTTP header filtering, and PathPrefix routing.
    *   `prober`: Executes `proberSteps`, testing the node prober functionality.
*   **`echo`**: Launches an HTTP server that returns received requests as JSON, along with TCP and UDP echo capabilities. This acts as the in-cluster backend behind the tunnels during tests.
*   **`acme-standin`**: Starts a hostNetwork listener on port :80. It responds with 404 to all requests, satisfying the expectations of `weft probe` for ACME HTTP-01 challenge validation on nodes.

### Key Components

*   **`runEcho`**: Handles the initialization of the echo backend services.
*   **`runACMEStandin`**: Manages the ACME challenge stand-in listener.
*   **`tunnelsSteps` / `proberSteps`**: Define the step-by-step logic for their respective e2e scenarios, ensuring comprehensive coverage of tunneling and probing features.