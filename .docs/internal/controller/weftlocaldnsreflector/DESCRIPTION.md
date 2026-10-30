<!-- docdaemon ts:2026-10-30T03:00:09Z hash:1524a6aff9782c55 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `weftlocaldnsreflector` package implements a cluster-scoped controller that resolves public hostnames internally to prevent traffic hairpinning through bastions.

## Overview
This directory contains the logic for the `WeftLocalDNSReflector`, which keeps public hostnames resolvable inside the cluster. It coordinates certificate issuance, proxy deployment, and CoreDNS configuration to route in-cluster traffic directly to WeftTunnels. The system ensures DNS consistency and TLS trust across all namespaces before activating overrides.

## Deep Dive
### Claim Resolution
The controller uses a deterministic "oldest wins" strategy to assign hostname ownership among multiple reflectors. Explicit claims take precedence over the `allHostnames` mode used by the operator-managed `cluster-default` reflector, preventing duplicate CoreDNS zones.

### PKI and Trust Management
TLS certificates are managed via Kubernetes Secrets, with automatic renewal triggered by SAN drift or expiry. A "trust gate" mechanism ensures DNS overrides are only applied after all running pods have received and trusted the CA bundle, preventing handshake failures during rollout.

### DNS and Proxy Synchronization
Routes derived from `WeftTunnel` resources are rendered into CoreDNS server blocks and injected into a shared ConfigMap. The controller reconciles a sidecar proxy Deployment and a node agent DaemonSet to handle TLS termination and `/etc/hosts` updates, ensuring both containerized and host-process resolution works correctly.

## Key Interfaces
- `Reconciler`: Drives the desired state for `WeftLocalDNSReflector` custom resources.
- `DefaultReflector`: Leader-only runnable that syncs the operator-managed default reflector.
- `ensureCertificates`: Manages the lifecycle of CA and serving Secrets.
- `syncCoreDNS`: Updates the shared CoreDNS ConfigMap with reflector-specific routes.
- `computeRoutes`: Derives stable proxy routes from WeftTunnel destinations.

## Dependencies
- Kubernetes API types and controller-runtime
- Internal PKI and trust management packages
- CoreDNS configuration utilities
- Envtest for integration testing

## Subcomponents
- `certs.go`: TLS certificate lifecycle management.
- `claims.go`: Hostname conflict resolution logic.
- `coredns.go`: CoreDNS configuration rendering and syncing.
- `default.go`: Operator-managed default reflector implementation.
- `names.go`: Naming conventions and label constants.
- `reconciler.go`: Main controller reconciliation loop.
- `routes.go`: Route derivation from WeftTunnels.
- `trust.go`: Trust bundle distribution and pod validation.
- `workloads.go`: Proxy and node agent resource reconciliation.