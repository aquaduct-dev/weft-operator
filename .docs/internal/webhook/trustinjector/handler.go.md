<!-- docdaemon ts:2026-10-29T20:36:46Z hash:db94cca48c68d46b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the mutating admission webhook handler that injects WeftLocalDNSReflector CA certificates into new pods to enable in-cluster DNS resolution.

The `Handler` struct processes admission requests for Pod creation. It decodes the incoming pod spec and checks for opt-out annotations via `trust.OptedOut`. If not opted out, it builds the current trust bundle using `trust.Source.Build`.

If the bundle is empty, the request is allowed unmodified. Otherwise, the handler ensures the namespace-specific ConfigMap containing the trust bundle exists by calling `CreateConfigMapIfMissing`, skipping this step during dry-run requests to avoid side effects.

The handler then attempts to inject the CA bundle into the pod's volumes and environment variables using `trust.InjectPod`. If injection occurs, the modified pod is marshaled and returned as a JSON patch response. Any errors during decoding, bundle building, or publishing result in an "Allowed" response with an unmodified pod, ensuring the webhook failure policy of `Ignore` prevents blocking pod creation.

The `podName` helper extracts the pod name from the request, handling both named and generate-name cases.