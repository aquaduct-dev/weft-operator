<!-- docdaemon ts:2026-10-29T20:36:44Z hash:2b5ebef96d4972bb model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the trust bundle construction and Kubernetes pod injection logic.

### Purpose
The tests verify that system root certificates are correctly merged with application-specific CAs, and that these bundles are safely injected into pods via volume mounts without altering environment variables or existing mount structures.

### Key Functions

*   **`TestBuild`**: Validates `Build` function behavior. It ensures system roots (minimum count enforced) are prepended to the bundle, custom CAs are sorted by reflector name, and the output is deterministic regardless of input order. It also rejects bundles with fewer than the minimum required system roots.
*   **`TestInjectPod`**: Verifies `InjectPod` modifies the pod spec correctly. It checks that a ConfigMap volume is added, annotations are updated with CA fingerprints, and specific system certificate paths are overlaid in all containers. It confirms that container environments remain untouched and that existing mounts under `/etc/ssl/certs` are not overwritten. Idempotence is tested to ensure repeated injections do not duplicate resources.
*   **`TestInjectSkips`**: Ensures injection is skipped when appropriate. This includes handling opt-out annotations, Windows node selectors, empty bundles, and cases where overlaying would conflict with a root filesystem mount (`MountPath: "/"`).
*   **`TestTrustsCA`**: Tests the helper function that checks if a specific CA fingerprint exists in the pod's injection annotation.