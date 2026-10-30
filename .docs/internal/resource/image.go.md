<!-- docdaemon ts:2026-10-30T02:59:58Z hash:c5016994e067bf1b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides helper functions to determine the default container image and appropriate pull policy for Weft operator components.

### Purpose
The `resource` package exposes utilities to standardize how Kubernetes pods are configured with images. It ensures that floating tags like `:latest` trigger aggressive re-pulling, while pinned or digest-based images rely on local caches to support air-gapped environments.

### Key Functions

**`WeftImageOr(image string) string`**
Returns the provided `image` if it is non-empty; otherwise, returns `DefaultWeftImage`. This allows configuration overrides while maintaining a sensible default (`ghcr.io/aquaduct-dev/weft:latest`).

**`PullPolicy(image string) corev1.PullPolicy`**
Determines the `imagePullPolicy` based on the image reference syntax:
- **Digests (`@`)**: Returns `IfNotPresent`. Images referenced by content hash are immutable and should not be re-fetched.
- **Floating Tags (`:latest` or untagged)**: Returns `Always`. Ensures the latest version is fetched, preventing stale execution.
- **Pinned Tags**: Returns `IfNotPresent`. Optimizes for offline/airgapped nodes where network access may be unavailable.

### Integration
These helpers are used when constructing `PodSpec` objects via `CreateOrUpdate`. Using explicit pull policies ensures idempotency: changing the image tag in the API triggers an update, but the pull behavior remains consistent with the tag's mutability characteristics.