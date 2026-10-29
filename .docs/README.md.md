<!-- docdaemon ts:2026-10-29T20:35:42Z hash:fdb60ba51f6739ed model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file documents the Weft Operator, a Kubernetes controller that automates exposing cluster services to the internet via WireGuard tunnels and local DNS reflection.

The operator manages several Custom Resource Definitions (CRDs) to handle network topology:

*   **`WeftServer`**: Deploys `weft server` on public nodes using host networking. It parses connection strings from `Spec.ConnectionString` and automatically discovers internet-routable nodes by running `weft probe`. If `Location` is `External`, it expects reconciliation by `AquaductTaaS` rather than creating a local deployment.
*   **`WeftTunnel`**: Represents a tunnel connecting a backend (`SrcUrl`) to a public endpoint (`DstUrl`). It injects secrets from referenced `WeftServer` resources into deployments, avoiding credential storage in the CRD. It supports load balancing across multiple servers via `TargetServers`.
*   **`WeftGateway`**: Implements the Kubernetes Gateway API, allowing standard `Gateway` resources to configure Weft-specific settings like server selection through a `GatewayClass`.
*   **`AquaductTaaS`**: Synchronizes cloud-hosted bastions from the aquaduct.dev API into the cluster as `WeftServer` resources with `Location: External`. It uses a long-lived access token stored in a Secret to mirror and suspend remote bastions.
*   **`WeftLocalDNSReflector`**: Keeps traffic for public hostnames within the cluster to avoid hairpinning. It issues TLS certificates, runs a proxy service, and injects trust bundles into pods via a mutating admission webhook. It also configures CoreDNS and node-level `/etc/hosts` entries to route traffic directly to in-cluster backends.

### Configuration & Secrets
Credentials are never stored in CRD specs. Instead, the operator references Secrets:
*   `Spec.CloudflareTokenSecretRef`: For DNS updates in `WeftServer`.
*   `Spec.AccessTokenSecretRef`: For authenticating with aquaduct.dev in `AquaductTaaS`.
*   Connection secrets are parsed from `WeftServer` connection strings or fetched dynamically for `WeftTunnel` injections.

### Installation
Install via Helm:
```bash
helm install weft-operator oci://ghcr.io/aquaduct-dev/charts/weft-operator:latest
```

### Testing
End-to-end tests run in an airgapped Firecracker microVM using Bazel:
```bash
bazel test --config=kvm //e2e/...
```