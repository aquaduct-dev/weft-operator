<!-- docdaemon ts:2026-10-30T02:59:21Z hash:5e89d818bbe1520c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `missingRouteKinds` function, verifying its ability to detect unsupported Gateway API route types.

### Purpose
The test suite validates logic that checks which Gateway API route kinds are missing from the cluster's discovery resources. It ensures the application correctly identifies unsupported routes based on the available API resource lists.

### Key Components
- **`TestMissingRouteKinds`**: A table-driven test covering three scenarios:
  - **Standard channel**: Verifies detection of missing TCP, TLS, and UDP routes when no v1alpha2 resources are present.
  - **Partial support**: Checks that only missing routes (TCP and UDP) are reported when only TLSRoute is served.
  - **Experimental channel**: Confirms an empty result when all expected routes (TCP, TLS, UDP) are present in v1alpha2.

### Integration
These tests ensure the reliability of the route kind validation logic used during initialization or configuration checks, preventing runtime errors due to unsupported API versions.