<!-- docdaemon ts:2026-10-29T20:38:55Z hash:fad7b7108f0ef55d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements the core Kubernetes controllers that manage Weft infrastructure, including bastion mirroring, DNS resolution, gateway translation, local DNS reflection, and tunnel provisioning.

## Overview
The `controller` package orchestrates the lifecycle of Weft’s networking components by reconciling external state with Kubernetes resources. It bridges cloud-hosted services, standard Gateway API definitions, and internal cluster traffic routing into a unified operational model. Controllers handle complex interactions such as JWT authentication, certificate management, and deterministic load balancing to ensure reliable connectivity.

## Deep Dive
### Bastion Mirroring and Domain Management
The `aquaducttaas` controller syncs external bastions from the aquaduct.dev API into `WeftServer` resources, skipping suspended or IP-less nodes to prevent connection failures. It manages domain ownership via Cloudflare, asserting write access through PATCH operations and handling foreign domain errors gracefully. Authentication uses a two-step flow exchanging long-lived tokens for short-lived JWTs, with caching to mitigate 401s during server-side rotation.

### Gateway Translation
The `weftgateway` controller translates Gateway API resources into internal `WeftTunnel` and `DNSRecord` objects. It enforces strict listener-route attachment to prevent duplicate tunnel creation and proxy conflicts. Layer 7 traffic fans out across eligible bastions, while Layer 4 traffic is pinned to a single bastion to avoid port collisions. The controller also handles header rewriting and automatic DNS record generation based on listener hostnames.

### Local DNS Reflection
The `weftlocaldnsreflector` resolves public hostnames inside the cluster using CoreDNS overrides and node-level `/etc/hosts` updates, avoiding hairpinning through bastions. It manages TLS certificates and trust bundles, enforcing a "trust gate" to prevent outages during rollout. A leader-only default reflector serves all published hostnames, improving performance for bulk operations like image pulls.

### Tunnel and Server Lifecycle
The `wefttunnel` controller establishes network tunnels between namespaces by creating dedicated agent Deployments per target server. It filters status-only updates to avoid unnecessary reconciliations and stabilizes availability conditions using `MinReadySeconds`. The `weftserver` controller provisions server instances on eligible nodes, probing connectivity and maintaining live Tx/Rx metrics via a cached HTTP client to prevent connection leaks.

## Key Interfaces
*   `AquaductTaaSReconciler`: Reconciles external bastions into `WeftServer` resources.
*   `DNSRecordReconciler`: Manages domain registration and DNS resolution.
*   `WeftGatewayReconciler`: Translates Gateway API resources into tunnels and DNS records.
*   `Reconciler` (weftlocaldnsreflector): Drives desired state for in-cluster DNS reflection.
*   `WeftServerReconciler`: Provisions and monitors WeftServer custom resources.
*   `WeftTunnelReconciler`: Manages tunnel agent Deployments and target connectivity.
*   `HTTPAPIClient`: Handles authenticated HTTP interactions with the aquaduct.dev API.
*   `NodeProber`: Discovers new nodes and provisions missing servers.

## Dependencies
*   `controller-runtime`: Kubernetes operator framework for reconciliation loops.
*   `gateway-api`: Standard Gateway API types and clients.
*   `//internal/pki`: PKI management for CA and certificate generation.
*   `//internal/trust`: Trust configuration and bundle construction.
*   Ginkgo/Gomega: Testing frameworks for suite execution.
*   Envtest: Local test environment setup for integration tests.

## Subcomponents
*   `aquaducttaas/`: Bastion mirroring and domain management controller.
*   `dnsrecord/`: DNSRecord controller for domain registration and resolution.
*   `suite_test.go`: Integration test environment setup.
*   `weftgateway/`: Gateway API to tunnel/DNS translation controller.
*   `weftlocaldnsreflector/`: In-cluster DNS reflection and trust management.
*   `weftserver/`: WeftServer provisioning and node discovery.
*   `wefttunnel/`: Tunnel agent deployment and connectivity management.