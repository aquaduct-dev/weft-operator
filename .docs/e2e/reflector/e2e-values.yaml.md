<!-- docdaemon ts:2026-10-29T20:35:55Z hash:326f791e1383ab79 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines Helm chart values for an airgapped end-to-end test environment where all container images are preloaded and no network pulls are permitted.

The configuration enforces strict offline constraints by setting `image.pullPolicy` to `Never`, ensuring the test suite relies exclusively on locally available artifacts. The primary image is tagged with `:e2e` to isolate testing from production releases, while the underlying `weftImage` also uses a dedicated e2e tag.

Horizontal Pod Autoscaling via VerticalPodAutoscaler is explicitly disabled (`enabled: false`) along with its controller installation, as the single-node k3s microVM environment does not require dynamic resource scaling. Similarly, the `gatewayClass.create` flag is set to `false` because the test validates in-cluster DNS resolution through the reflector rather than external ingress routing. This setup supports the firecracker-based e2e tests that verify traffic remains within the cluster boundaries without relying on external registries or public DNS.