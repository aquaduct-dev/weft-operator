<!-- docdaemon ts:2026-10-30T02:59:25Z hash:0d0945145d793814 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file serves as the entry point for the `aquaducte2e` binary, orchestrating either an end-to-end test scenario or a local API mock.

The binary operates in two distinct modes determined by command-line arguments:

1.  **Scenario Mode (Default)**: When executed without arguments, it runs on the firecracker microVM alongside k3s. It utilizes `e2ekit.Main` to drive the "aquaduct" test scenario, which validates the operator's behavior against real HTTP clients and in-cluster services.
2.  **Fake Mode**: When invoked with the `fake` argument, it starts an in-memory fake implementation of the aquaduct.dev API and authorization token exchange. This mode is used to provide stable, predictable responses during e2e tests, handling JWT validation and status code simulation without external dependencies.

The `main` function checks `os.Args[1]` to dispatch execution to either `runFake` or the standard e2e kit runner.