<!-- docdaemon ts:2026-10-30T02:59:46Z hash:00577f2a54dc938f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `AquaductTaaSReconciler`, which mirrors cloud-hosted bastions from aquaduct.dev into the cluster as WeftServers and manages their lifecycle.

The reconciler polls the aquaduct.dev API to synchronize external bastions, creating or updating corresponding `WeftServer` resources in the same namespace. It uses a label (`weft.aquaduct.dev/aquaducttaas`) to track owned servers and prunes any that are no longer returned by the API. The status subresource is updated only when actual state changes or the sync heartbeat becomes stale, preventing unnecessary resource version bumps and watch loops.

Deletion handling is strict: it first cascades-delete dependent `DNSRecord` objects (which rely on this TaaS for domain cleanup), then suspends all managed bastions via the API to stop billing, releases the pin on the access-token Secret, and finally removes its own finalizer. A secondary finalizer on the token Secret prevents deletion while any `AquaductTaaS` in the namespace still references it, avoiding wedged cleanup paths.

Key types include `AquaductTaaSReconciler`, which embeds the controller-runtime client and an injected `APIClient`. Constants define labels, annotations, and finalizers used for ownership tracking and safe deletion.