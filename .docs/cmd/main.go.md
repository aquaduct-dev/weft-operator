<!-- docdaemon ts:2026-10-30T02:59:21Z hash:5c5de372205f4119 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file is the entry point for the Weft Operator, initializing the controller manager, registering reconcilers, and configuring webhooks.

### Initialization
The `main` function bootstraps a `controller-runtime` manager with leader election and health probes. It registers schemes for core Kubernetes types, Gateway API v1/v1alpha2, and custom Weft CRDs. The binary also supports a `node-agent` subcommand to run as a DaemonSet without requiring a separate image.

### Preflight Checks
Before starting controllers, the code verifies that Gateway API CRDs are installed in the cluster. If missing, it exits with a clear error rather than failing later during informer cache synchronization.

### Controller Setup
Six reconcilers are registered with the manager:
- **WeftServer**: Mirrors external bastions from the Aquaduct TaaS API.
- **WeftTunnel**: Manages tunnel resources.
- **AquaductTaaS & DNSRecord**: Handle authentication token exchange and domain registration status.
- **WeftGateway**: Reconciles Gateway API resources, ignoring optional route kinds (TCP/TLS/UDP) if their CRDs are absent.
- **WeftLocalDNSReflector**: Keeps public hostnames resolvable within the cluster.

### Webhook Configuration
A trust injection webhook is configured to inject CA certificates into new pods, excluding the operator’s own namespace and `kube-system`. Certificate renewal is handled via a background manager add-on.

### Cache Optimization
The `reflectorOwnedOnly` function restricts the ConfigMap informer to only those labeled as managed by the reflector, preventing memory exhaustion from caching all cluster ConfigMaps.