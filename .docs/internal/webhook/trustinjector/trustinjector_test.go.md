<!-- docdaemon ts:2026-10-29T20:36:21Z hash:212b5392c7d246c6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the trust webhook handler and its setup logic, verifying certificate injection, bundle publishing, and webhook configuration stability.

The test suite validates `Handler.Handle` behavior across three scenarios: dry-run requests (which patch pods but do not publish the CA bundle ConfigMap), standard creation requests (which patch pods and publish the bundle), and opted-out pods (which remain unpatched). It ensures that the injected patches correctly reference the trust bundle and that the bundle contains the expected number of certificates from system roots and local reflectors.

`TestHandlerWithoutReflectors` confirms that in the absence of `WeftLocalDNSReflector` resources, pods are admitted without modification, ensuring the webhook is non-disruptive when no trust data is available.

`TestSetup` verifies the initialization of the `MutatingWebhookConfiguration`, checking that namespace exclusions (e.g., `kube-system`) are applied correctly and that the webhook points to the correct service path. It also validates the lifecycle of the serving certificate: confirming that restarts reuse the same CA and certificate, while renewals within the specified window generate a new leaf certificate signed by the existing CA.