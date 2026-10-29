<!-- docdaemon ts:2026-10-29T20:39:42Z hash:b9df603cd617bf70 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `internal` package provides the core infrastructure, controllers, and utilities that power the Weft Operator’s networking, security, and testing capabilities.

## Overview
This directory orchestrates the lifecycle of Weft’s networking components by reconciling external state with Kubernetes resources. It bridges cloud-hosted services, standard Gateway API definitions, and internal cluster traffic routing into a unified operational model. The codebase handles complex interactions such as JWT authentication, certificate management, and deterministic load balancing to ensure reliable connectivity.

## Deep Dive
### Bastion Mirroring and Domain Management
The `aquaducttaas` controller syncs external bastions from the aquaduct.dev API into `WeftServer` resources, skipping suspended or IP-less nodes to prevent connection failures. It manages domain ownership via Cloudflare, asserting write access through PATCH operations and handling foreign domain errors gracefully. Authentication uses a two-step flow exchanging long-lived tokens for short-lived JWTs, with caching to mitigate 401s during server-side rotation. Recent fixes addressed status-write loops that caused excessive reconciliation rates by ensuring updates only occur when actual drift is detected.

### Gateway Translation
The `weftgateway` controller translates Gateway API resources into internal `WeftTunnel` and `DNSRecord` objects. It enforces strict listener-route attachment to prevent duplicate tunnel creation and proxy conflicts. Layer 7 traffic fans out across eligible bastions, while Layer 4 traffic is pinned to a single bastion to avoid port collisions. The controller also handles header rewriting and automatic DNS record generation based on listener hostnames.

### Local DNS Reflection
The `weftlocaldnsreflector` resolves public hostnames inside the cluster using CoreDNS overrides and node-level `/etc/hosts` updates, avoiding hairpinning through bastions. It manages TLS certificates and trust bundles, enforcing a "trust gate" to prevent outages during rollout. A leader-only default reflector serves all published hostnames, improving performance for bulk operations like image pulls. The `nodeagent` DaemonSet configures node-level DNS and TLS trust to route specific public hostnames through this in-cluster proxy, managing `/etc/hosts` entries and containerd certificate trust stores atomically.

### Tunnel and Server Lifecycle
The `wefttunnel` controller establishes network tunnels between namespaces by creating dedicated agent Deployments per target server. It filters status-only updates to avoid unnecessary reconciliations and stabilizes availability conditions using `MinReadySeconds`. The `weftserver` controller provisions server instances on eligible nodes, probing connectivity and maintaining live Tx/Rx metrics via a cached HTTP client to prevent connection leaks.

### PKI and Trust Injection
The `pki` package generates and signs X.509 certificates, enforcing strict SAN requirements and handling renewal logic. The `trust` package assembles CA trust bundles from system roots and reflector CAs, injecting them into pods via mutating admission webhooks. This ensures workloads can verify TLS connections to public hostnames resolved internally without modifying workload specs. The `webhook` handler intercepts pod creation events, checking opt-out annotations and mounting the trust bundle ConfigMap over standard certificate paths.

### Testing Infrastructure
The `clusterharness` package provides the infrastructure for running end-to-end tests in isolated firecracker microVMs or against live Kubernetes clusters. It defines a stable JSON reporting contract for parsing results via `jq` on the host side. `AcquireNamespace` resolves the target environment, prioritizing Firecracker hermetic mode for airgapped validation of operator behavior.

## Key Interfaces
*   `AquaductTaaSReconciler`: Reconciles external bastions into `WeftServer` resources.
*   `DNSRecordReconciler`: Manages domain registration and DNS resolution.
*   `WeftGatewayReconciler`: Translates Gateway API resources into tunnels and DNS records.
*   `Reconciler` (weftlocaldnsreflector): Drives desired state for in-cluster DNS reflection.
*   `WeftServerReconciler`: Provisions and monitors WeftServer custom resources.
*   `WeftTunnelReconciler`: Manages tunnel agent Deployments and target connectivity.
*   `HTTPAPIClient`: Handles authenticated HTTP interactions with the aquaduct.dev API.
*   `NodeProber`: Discovers new nodes and provisions missing servers.
*   `Agent`: Core loop controller for node-level DNS and TLS configuration.
*   `NewCA`: Generates a fresh self-signed ECDSA P-256 CA certificate.
*   `IssueLeaf`: Signs a serving certificate using an existing CA.
*   `Build`: Assembles the combined trust bundle from system roots and reflector CAs.
*   `EnsureConfigMap`: Creates or updates the trust bundle ConfigMap.
*   `InjectPod`: Mutates a pod spec to mount the trust bundle.
*   `Handler.Handle`: Processes admission requests for Pod creation.
*   `Report`: Struct defining the overall scenario outcome for e2e tests.
*   `Runner`: Interface for executing test scenarios and returning results.

## Dependencies
*   `controller-runtime`: Kubernetes operator framework for reconciliation loops.
*   `gateway-api`: Standard Gateway API types and clients.
*   `//internal/pki`: PKI management for CA and certificate generation.
*   `//internal/trust`: Trust configuration and bundle construction.
*   Ginkgo/Gomega: Testing frameworks for suite execution.
*   Envtest: Local test environment setup for integration tests.
*   `logr`: Logging infrastructure used by the agent and tests.
*   Standard library (`crypto/x509`, `encoding/json`, `os`, etc.)

## Subcomponents
*   `clusterharness/`: E2E test infrastructure and reporting contract.
*   `controller/aquaducttaas/`: Bastion mirroring and domain management.
*   `controller/dnsrecord/`: DNSRecord controller for domain registration.
*   `controller/weftgateway/`: Gateway API to tunnel/DNS translation.
*   `controller/weftlocaldnsreflector/`: In-cluster DNS reflection.
*   `controller/weftserver/`: WeftServer provisioning and node discovery.
*   `controller/wefttunnel/`: Tunnel agent deployment management.
*   `nodeagent/`: DaemonSet for node-level DNS and TLS trust.
*   `pki/`: Cryptographic utilities for X.509 certificate lifecycles.
*   `resource/`: Generic reconciliation framework for resource lifecycles.
*   `trust/`: CA trust bundle assembly and pod injection.
*   `webhook/`: Mutating admission webhook for trust injection.