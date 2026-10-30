<!-- docdaemon ts:2026-10-30T02:59:37Z hash:229d1d45c88e1ff1 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides shared helper functions and state management for e2e tests covering WeftLocalDNSReflector claims and lifecycle scenarios.

### Purpose
It bridges the `e2ekit` testing framework with specific reflector logic, allowing `claims_scenario.go` and `lifecycle_scenario.go` to reuse a common registry backend while asserting on node-level trust state.

### Key Components

**State Management (`extras`)**
Holds the test kit and legacy scenario helpers. `install` backs up the pristine node CA bundle before installing the operator, ensuring clean state for assertions.

**Reflector Operations**
- `createReflector`, `patchReflector`: Manage the custom resource spec.
- `waitStatus`: Polls until conditions match, using `condIs` and `condSummary` to validate status fields like `ReflectedHostnames` or `UntrustedPods`.
- `diagnostics`: Logs full reflector status, CoreDNS keys, and node agent logs on failure.

**Node & Trust Assertions**
- `nodeTrustHasCA`, `nodeHostsLine`: Read `/etc/hosts` and the system CA bundle to verify injected entries and certificates.
- `removeBlock`: Simulates manual cleanup of these blocks to test idempotency or removal logic.
- `caFingerprint`, `servingCert`: Extract certificate fingerprints from Secrets for verification.

**Infrastructure Helpers**
- `restartRegistry`: Deletes registry pods and waits for them to restart with updated trust annotations.
- `clientPod`: Runs busybox clients with retries to handle CoreDNS reload delays.
- `proxyDeployment`, `proxyPods`: Inspect the proxy sidecar components for health checks.