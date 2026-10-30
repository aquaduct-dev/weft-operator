<!-- docdaemon ts:2026-10-30T03:02:29Z hash:40110f6140bede97 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `internal` package contains the core operator logic, infrastructure testing harnesses, and cryptographic utilities required to manage Weft’s distributed network.

## Overview
This directory orchestrates the lifecycle of custom resources like `WeftServer`, `WeftTunnel`, and `DNSRecord` by reconciling desired states with cluster conditions. It bridges external cloud APIs and Kubernetes Gateway API constructs into internal proxy deployments, ensuring secure traffic routing through bastions. Additionally, it provides the node-level agent for DNS reflection and a mutating webhook for trust injection, enabling efficient in-cluster resolution without public internet traversal.

## Deep Dive
### Controller Orchestration
Controllers manage the synchronization between external services and internal resources. The `AquaductTaaS` controller mirrors bastion servers, handling JWT authentication and ordered deletion to prevent orphaned resources. The `weftgateway` and `wefttunnel` controllers translate Gateway API routes into tunnel deployments, distinguishing between Layer 7 fan-out and Layer 4 pinned protocols. The `weftserver` controller auto-provisions servers on nodes with public connectivity and caches HTTP clients to prevent connection leaks during status polling.

### Local DNS Reflection
To optimize bulk transfers, the `weftlocaldnsreflector` resolves public hostnames internally, preventing traffic hairpinning. It coordinates certificate issuance and CoreDNS configuration, using a "trust gate" to validate pod readiness before activating overrides. The `nodeagent` DaemonSet configures `/etc/hosts` and containerd TLS trust stores, periodically verifying proxy health and reverting changes on failure to ensure safe fallback to public routing.

### Trust and Security
The `pki` package manages X.509 certificate lifecycles, generating self-signed CAs and issuing leaf certificates with strict SAN enforcement. The `trust` package aggregates these CAs with system roots, publishing them as a ConfigMap. A mutating admission webhook injects this bundle into pods, overriding standard certificate paths to ensure workload trust without image modifications, while respecting opt-out annotations.

### Testing Infrastructure
The `clusterharness` package supports end-to-end testing in isolated firecracker microVMs or live clusters. It defines a stable JSON reporting contract for parsing test outcomes via shell scripts, allowing self-contained validation of operator behavior in airgapped environments.

## Key Interfaces
*   `AquaductTaaSReconciler`: Syncs bastions and domains via the aquaduct.dev API.
*   `WeftGatewayReconciler`: Translates Gateway API routes into WeftTunnels.
*   `WeftLocalDNSReflector`: Resolves public hostnames inside the cluster.
*   `Agent`: Core loop controller for node-level DNS and TLS configuration.
*   `NewCA`: Generates a fresh self-signed ECDSA P-256 CA certificate.
*   `InjectPod`: Mutates a pod spec to mount the trust bundle volume.
*   `Handler.Handle`: Processes admission requests to inject trust bundles.
*   `Runner`: Interface for executing test scenarios and returning results.

## Dependencies
*   `controller-runtime`: Kubernetes controller framework.
*   `client-go`: Standard Kubernetes client libraries.
*   `gateway-api`: Kubernetes Gateway API types.
*   `aquaduct.dev/weft-operator/internal/lab`: Lab environment definitions.
*   Go standard library (`crypto/x509`, `encoding/json`, `os`).

## Subcomponents
*   `clusterharness/`: E2E test infrastructure and reporting.
*   `controller/`: Core Kubernetes controllers.
*   `nodeagent/`: Node-level DNS and TLS configuration.
*   `pki/`: Cryptographic utilities and certificate management.
*   `resource/`: Generic reconciliation logic and image policies.
*   `trust/`: CA bundle assembly and pod injection.
*   `webhook/`: Mutating admission webhook for trust injection.