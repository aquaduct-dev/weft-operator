<!-- docdaemon ts:2026-10-29T20:37:01Z hash:1d0aed32c2098b59 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the logic to inject a CA trust bundle into Kubernetes pods via volume mounts, enabling workload trust without image changes.

The `InjectPod` function mutates a pod spec to mount a ConfigMap containing trusted CAs over standard system certificate paths (`/etc/ssl/certs/ca-certificates.crt`, etc.). It handles both init and regular containers, ensuring idempotency by checking for existing mounts or volumes.

Key behaviors:
- **Opt-out**: Pods with label/annotation `weft.aquaduct.dev/inject-trust=false` are skipped.
- **Platform constraints**: Injection is disabled for Windows nodes where these Linux paths do not apply.
- **Mount Logic**: For each container, it checks if any existing volume mount covers a target path (including parent directories). If not, it adds a read-only subPath mount from the `weft-trust-bundle` ConfigMap.
- **Tracking**: The annotation `weft.aquaduct.dev/trusted-cas` records the fingerprints of injected CAs for verification.

Supporting functions include `OptedOut`, `IsWindows`, `mountCovers` (to prevent duplicate/conflicting mounts), and `TrustsCA` (to verify if a specific fingerprint was injected).