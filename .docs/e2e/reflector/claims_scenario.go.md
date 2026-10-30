<!-- docdaemon ts:2026-10-30T02:59:36Z hash:c316d595cd4062f9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the `claims` end-to-end test scenario, verifying how multiple reflectors compete for hostname ownership and how trust gates affect DNS publication.

### Purpose
The `claimsScenario` struct orchestrates a sequence of steps to validate conflict resolution logic between reflectors with different scopes (`allHostnames` vs explicit) and ages (older wins). It also tests the `ignoreUntrustedPods` configuration's impact on the trust gate and DNS availability.

### Key Components
- **`claimsSteps`**: Returns the ordered list of test steps, from operator installation through workload creation, route setup, trust gate toggling, and finally claim handover upon deletion.
- **`workloads`**: Deploys legacy pods that predate reflectors, including those opted out via label/annotation or residing in `kube-system`, to test trust gate exemptions.
- **`routes`**: Establishes HTTPS and HTTP tunnels while ensuring unrouted hosts (TCP, loopback, missing) are correctly reported in `status.unroutedHostnames`.
- **`trustGate`**: Validates that disabling `ignoreUntrustedPods` closes the trust gate on untrusted legacy pods, withdrawing DNS keys until they are restarted or deleted.
- **`handover`**: Confirms that deleting the winning reflector transfers hostname claims and DNS keys to the loser, while the `allHostnames` reflector reclaims any remaining hostnames.