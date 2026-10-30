<!-- docdaemon ts:2026-10-30T03:01:08Z hash:4957d981001594d4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the core Kubernetes controllers that manage Weft infrastructure, including bastion mirroring, tunnel provisioning, gateway translation, and local DNS reflection.

## Overview
The `controller` package orchestrates the lifecycle of custom resources (`WeftServer`, `WeftTunnel`, `DNSRecord`) by reconciling their desired state with actual cluster conditions. It bridges external cloud APIs and standard Kubernetes Gateway API constructs into internal proxy deployments, ensuring traffic is routed securely through bastions while maintaining efficient in-cluster resolution.

## Deep Dive
### Bastion Synchronization
The `AquaductTaaS` controller mirrors external bastion servers from the aquaduct.dev API into `WeftServer` CRs. It handles two-step JWT authentication and caches tokens to prevent expiry races. Deletion logic is strictly ordered: dependent DNS records are removed first, then remote bastions are suspended to stop billing, and finally access-token secrets are released. This prevents orphaned resources and wedged finalizers.

### Tunnel and Gateway Management
The `weftgateway` controller translates Gateway API resources into `WeftTunnel` CRs. It distinguishes between Layer 7 (fan-out) and Layer 4 (pinned) protocols, constructing appropriate URLs for each. The `wefttunnel` controller then ensures Deployments exist for these tunnels, using idempotent mutation strategies to avoid infinite update loops caused by API server defaults.

### Local DNS Reflection
The `weftlocaldnsreflector` resolves public hostnames internally to prevent traffic hairpinning. It coordinates certificate issuance and CoreDNS configuration, ensuring TLS trust across namespaces before activating overrides. A "trust gate" mechanism prevents handshake failures during rollout by validating pod readiness.

### Server Lifecycle
The `weftserver` controller manages underlying infrastructure like Deployments and PVCs. It includes a background node prober that discovers nodes with public connectivity and auto-provisions servers. Client caching is used to prevent connection leaks during frequent status polling.

## Key Interfaces
- `AquaductTaaSReconciler`: Syncs bastions and domains via the aquaduct.dev API.
- `DNSRecordReconciler`: Manages domain registration and ownership assertions.
- `WeftGatewayReconciler`: Translates Gateway API routes into WeftTunnels.
- `WeftLocalDNSReflector`: Resolves public hostnames inside the cluster.
- `WeftServerReconciler`: Manages server deployments and metrics.
- `WeftTunnelReconciler`: Provisions tunnel Deployments for target servers.
- `NodeProber`: Discovers nodes and provisions servers automatically.

## Dependencies
- `controller-runtime`: Kubernetes controller framework.
- `client-go`: Standard Kubernetes client libraries.
- `gateway-api`: Kubernetes Gateway API types.
- `envtest`: Testing environment for integration tests.
- `Ginkgo/Gomega`: BDD testing framework.

## Subcomponents
- `aquaducttaas/`: Bastion mirroring and domain registration.
- `dnsrecord/`: DNS record lifecycle management.
- `weftgateway/`: Gateway API to WeftTunnel translation.
- `weftlocaldnsreflector/`: In-cluster DNS resolution and TLS.
- `weftserver/`: Server deployment and node probing.
- `wefttunnel/`: Tunnel deployment management.
- `suite_test.go`: Integration test setup.
- `BUILD.bazel`: Build rules for tests.