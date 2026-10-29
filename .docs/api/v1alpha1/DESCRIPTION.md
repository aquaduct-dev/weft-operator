<!-- docdaemon ts:2026-10-29T20:38:37Z hash:7da86fa38974c599 model:openrouter/qwen/qwen3.7-flash -->
# api/v1alpha1

## TL;DR
This directory defines the Kubernetes Custom Resource Definitions (CRDs) and API types for the Weft operator, managing bastion infrastructure, DNS routing, and local traffic reflection.

## Overview
The `api/v1alpha1` package serves as the contract between the Kubernetes control plane and the external aquaduct.dev services. It declares the schema for resources that mirror cloud-hosted bastions, register domain names, and optimize internal cluster traffic by reflecting public hostnames locally. These types enable controllers to reconcile desired states with external APIs while maintaining a canonical source of truth within the cluster.

## Deep Dive

### Infrastructure Mirroring
The `AquaductTaaS` resource links a Kubernetes cluster to an aquaduct.dev account. It stores authentication credentials securely via secret references and triggers the mirroring of cloud-hosted bastions into the cluster as `WeftServer` objects with `Location=External`. This ensures downstream components have immediate access to bastion metadata without querying external APIs directly.

### Domain Registration
`DNSRecord` manages FQDN registrations at the aquaduct.dev provider. It enforces immutability on the domain name to allow finalizers to release old resources cleanly. The reconciler drives status conditions (`Registered`, `Resolved`, `Ready`) by comparing expected IPs derived from bastion data against live DNS lookups, handling propagation delays and conflicts.

### Local Traffic Reflection
`WeftLocalDNSReflector` is a cluster-scoped resource designed to keep public Weft traffic inside the cluster, preventing hairpinning through bastions. It configures CoreDNS overrides and deploys agents to inject CA certificates into nodes. By serving hostnames in-cluster, it optimizes bulk transfers like image pulls against internal registries.

### Tunnel and Gateway Management
`WeftTunnel` and `WeftGateway` define the connectivity layer. Tunnels specify source/destination URLs and target servers, while gateways provide parameter references for gateway classes. `WeftServer` tracks individual server configurations, including connection strings and Cloudflare tokens, exposing tunnel metrics and health conditions.

## Key Interfaces
*   `AquaductTaaS`: Primary CRD linking the cluster to aquaduct.dev, managing bastion mirroring.
*   `DNSRecord`: CRD for registering and resolving domains via the aquaduct.dev API.
*   `WeftLocalDNSReflector`: Cluster-scoped CRD for keeping public hostnames internal to the cluster.
*   `WeftServer`: CRD representing individual bastion servers, both internal and external.
*   `WeftTunnel`: CRD defining specific tunnel connections between sources and destinations.
*   `WeftGateway`: CRD specifying routing targets for tunnel traffic.
*   `AddToScheme`: Function to register all v1alpha1 types with the controller-runtime scheme.

## Dependencies
*   `k8s.io/api/core/v1`: For Secret references and standard Kubernetes primitives.
*   `k8s.io/apimachinery/pkg/apis/meta/v1`: For standard metadata and condition structures.
*   `k8s.io/apimachinery/pkg/runtime`: For serialization and scheme management.
*   `sigs.k8s.io/controller-runtime/pkg/scheme`: For building the API group version scheme.

## Subcomponents
*   `BUILD.bazel`: Bazel build rules for code generation and library compilation.
*   `aquaducttaas_types.go`: Types for AquaductTaaS and BastionInfo.
*   `dnsrecord_types.go`: Types for DNSRecord and its status conditions.
*   `groupversion_info.go`: API group registration and scheme builder initialization.
*   `weftgateway_types.go`: Types for WeftGateway and TargetServers.
*   `weftlocaldnsreflector_types.go`: Types for WeftLocalDNSReflector and ReflectedRoute.
*   `weftserver_types.go`: Types for WeftServer and TunnelStatus.
*   `wefttunnel_types.go`: Types for WeftTunnel and WeftTunnelList.