<!-- docdaemon ts:2026-10-29T20:37:11Z hash:6daf04e6ad346bd8 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `weftlocaldnsreflector` controller manages in-cluster DNS resolution for public hostnames by coordinating TLS certificates, a local proxy, and CoreDNS overrides.

## Overview
This package implements the `WeftLocalDNSReflector` custom resource, which routes traffic from inside the cluster to Weft-published services without hairpinning through a bastion. It ensures that hostnames are resolved locally via CoreDNS or node-level `/etc/hosts` updates, improving performance for bulk operations like image pulls. The controller handles the full lifecycle of trust, including CA issuance, certificate renewal, and pod injection, while enforcing a "trust gate" to prevent outages during rollout.

## Deep Dive

### Claim Resolution and Routing
To prevent duplicate zones, the system uses an "oldest wins" strategy to assign hostname ownership among multiple reflectors. The `claims.go` file resolves conflicts between explicit hostname claims and the new `allHostnames` mode used by the operator-managed `cluster-default` reflector. Routes are derived from `WeftTunnel` resources, filtering invalid schemes and ensuring deterministic output for the proxy configuration.

### Certificate and Trust Management
TLS security is managed through `certs.go` and `trust.go`. The controller ensures a self-signed CA and serving certificates exist as Secrets, renewing them when SANs drift or expiry approaches. The trust bundle is distributed to all namespaces via ConfigMaps. A critical "trust gate" logic prevents DNS overrides from activating until all running pods have been injected with the CA bundle, avoiding TLS handshake failures during updates.

### Proxy and Node Agent Deployment
The reconciler deploys a sidecar proxy (`ensureProxy`) that terminates TLS and forwards traffic through tunnels, and a DaemonSet (`ensureNodeAgent`) that updates `/etc/hosts` on nodes. The proxy scales to zero if no routes exist. The node agent runs with minimal capabilities to securely edit host files. Both components rely on idempotent creation/update patterns to preserve API defaults.

### CoreDNS Integration
`coredns.go` renders server blocks mapping hostnames to the proxy’s ClusterIP. These are injected into a shared CoreDNS ConfigMap using specific keys. The controller uses an uncached reader to bypass manager caching limitations and relies on resource version checks to avoid clobbering concurrent changes made by other controllers.

## Key Interfaces
- `Reconciler`: The main controller type driving the desired state for `WeftLocalDNSReflector` resources.
- `DefaultReflector`: Leader-only runnable that syncs the operator-managed `cluster-default` reflector.
- `resolveClaims`: Determines hostname ownership among reflectors using creation time and name.
- `renderCoreDNS`: Generates valid CoreDNS server block configurations for given hostnames.
- `computeRoutes`: Maps `WeftTunnel` destinations to proxy routes, handling scheme validation.
- `publishTrustBundle`: Distributes the CA bundle ConfigMap across active namespaces.

## Dependencies
- `//internal/pki`: PKI management for CA and certificate generation.
- `//internal/trust`: Trust configuration and bundle construction.
- Kubernetes APIs: `core/v1`, `apps/v1`, and controller-runtime framework.
- Envtest: Used for integration testing with etcd and kube-apiserver.

## Subcomponents
- `BUILD.bazel`: Bazel build targets for library and tests.
- `certs.go`: Lifecycle management of CA and serving TLS Secrets.
- `claims.go`: Conflict-resolution logic for hostname ownership.
- `coredns.go`: CoreDNS ConfigMap synchronization and rendering.
- `default.go`: Operator-managed `cluster-default` reflector logic.
- `names.go`: Centralized naming conventions, labels, and paths.
- `pure_test.go`: Unit tests for helper functions and routing.
- `reconciler.go`: Main reconciliation loop and subsystem coordination.
- `reconciler_test.go`: End-to-end integration tests using envtest.
- `routes.go`: Derivation of proxy routes from WeftTunnels.
- `suite_test.go`: Test environment bootstrapping.
- `trust.go`: Trust bundle distribution and untrusted pod detection.
- `workloads.go`: Reconciliation of Proxy Deployment, Service, and Node Agent DaemonSet.