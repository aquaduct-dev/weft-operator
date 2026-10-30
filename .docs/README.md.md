<!-- docdaemon ts:2026-10-30T02:59:21Z hash:275c53244f2939e3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file documents the Weft Operator, a Kubernetes controller that automates exposing internal services to the internet via WireGuard tunnels.

The operator manages `WeftServer` (bastion nodes), `WeftTunnel` (service proxies), and `WeftGateway` (Gateway API integration) resources. It invokes the `weft` CLI container to handle traffic routing between public endpoints and cluster-internal backends.

Key components include:
*   **WeftServer**: Probes nodes for internet routability and manages bastion deployments using connection secrets stored in `Spec.ConnectionString`.
*   **WeftTunnel**: Routes traffic from servers to specific `srcURL`/`dstURL` pairs, injecting credentials from associated servers.
*   **AquaductTaaS**: Mirrors cloud-hosted bastions into the cluster and handles lifecycle management via the aquaduct.dev API.
*   **WeftLocalDNSReflector**: Optimizes intra-cluster traffic by serving public hostnames locally via a proxy, avoiding hairpinning through the internet. It manages TLS certificates and injects trust bundles into pods via a mutating admission webhook.

Installation is performed via Helm from the OCI registry. End-to-end tests use Bazel and Firecracker microVMs to validate tunneling, DNS reflection, and trust injection in an airgapped environment.