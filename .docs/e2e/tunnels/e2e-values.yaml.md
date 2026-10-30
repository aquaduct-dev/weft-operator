<!-- docdaemon ts:2026-10-30T02:59:42Z hash:b725d6a90294cd07 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides Helm chart values for the airgapped e2e microVM environment, ensuring images are preloaded and disabling default reflectors to isolate tunnel tests.

### Purpose
The configuration supports end-to-end testing of Weft servers and tunnels within an airgapped k3s cluster. It enforces strict image pull policies to prevent network dependencies during test execution.

### Key Settings

*   **Image Management**:
    *   `image.url` and `weftImage`: Point to specific `e2e` tagged images (`ghcr.io/aquaduct-dev/weft-operator:e2e` and `ghcr.io/aquaduct-dev/weft:e2e`).
    *   `image.pullPolicy: Never`: Ensures no images are pulled from remote registries, relying entirely on preloaded local images.

*   **Resource Control**:
    *   `verticalPodAutoscaler.enabled: false`: Disables VPA to maintain stable resource allocation during tests.
    *   `defaultReflector.enabled: false`: Prevents the cluster-default reflector from reacting to resources created by tunnel tests, avoiding interference with test assertions.

*   **Networking**:
    *   `gatewayClass.create: true`: Enables the creation of the GatewayClass required for the tunnel ingress routing logic.

### Context
These values are used by the `tunnels_test` suite to validate HTTP, TCP, and UDP traffic flow through real `weft server` and `weft tunnel` pods. The isolation provided by disabling the reflector and enforcing `Never` pull policies ensures deterministic test outcomes in the airgapped environment.