<!-- docdaemon ts:2026-10-30T03:01:08Z hash:47c02e5f7ffdc96c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory defines the v1alpha1 Custom Resource Definitions for the Weft operator, managing infrastructure state and external integrations.

## Overview
The `api/v1alpha1` package establishes the schema for all custom resources managed by the operator. It bridges the Kubernetes cluster with the aquaduct.dev API, handling authentication, bastion mirroring, DNS registration, and local traffic reflection. These types enable controllers to reconcile desired states against external cloud services and internal cluster configurations.

## Deep Dive
### AquaductTaaS Integration
The `AquaductTaaS` resource authenticates with aquaduct.dev using a long-lived token exchanged for a short-lived JWT. It mirrors cloud-hosted bastions as `WeftServer` objects with `Location=External`, providing a unified view of infrastructure for downstream reconcilers.

### DNS Management
`DNSRecord` manages domain registrations via the aquaduct.dev API. It ensures domains are registered, resolves IPs from bastion data, and tracks propagation status through conditions like `Registered` and `Resolved`. The domain name is immutable to allow finalizers to release old names cleanly.

### Local DNS Reflection
`WeftLocalDNSReflector` keeps public Weft traffic inside the cluster by terminating TLS at an in-cluster proxy. It configures CoreDNS overrides and injects CA certificates into nodes, preventing hairpinning through bastions for bulk operations like image pulls.

### Tunnel and Gateway Configuration
`WeftTunnel` and `WeftGateway` define routing rules and tunnel endpoints. `WeftServer` stores connection details and location metadata, while `WeftGateway` references these servers to configure gateway classes.

## Key Interfaces
- `AquaductTaaS`: Main CRD for aquaduct.dev integration and bastion mirroring.
- `DNSRecord`: CRD for managing external domain registrations and resolution.
- `WeftLocalDNSReflector`: Cluster-scoped CRD for in-cluster DNS proxy configuration.
- `WeftServer`: CRD representing server instances, both internal and external.
- `WeftTunnel`: CRD defining tunnel source/destination URLs and targets.
- `WeftGateway`: CRD specifying target servers for tunnel routing.
- `AddToScheme`: Function to register all types with the controller-runtime scheme.

## Dependencies
- Kubernetes client-go libraries (`core/v1`, `meta/v1`, `runtime`)
- Controller-runtime scheme package
- Controller-gen tooling for CRD manifest generation

## Subcomponents
- `aquaducttaas_types.go`: AquaductTaaS resource definitions.
- `dnsrecord_types.go`: DNSRecord resource definitions.
- `weftgateway_types.go`: WeftGateway resource definitions.
- `weftlocaldnsreflector_types.go`: WeftLocalDNSReflector resource definitions.
- `weftserver_types.go`: WeftServer resource definitions.
- `wefttunnel_types.go`: WeftTunnel resource definitions.
- `groupversion_info.go`: API group and version registration.