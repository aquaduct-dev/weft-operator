<!-- docdaemon ts:2026-10-29T20:38:34Z hash:dbdc3e0f63ecb308 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `trust` package manages the assembly of CA trust bundles and their injection into Kubernetes pods to enable secure internal DNS reflection without workload modifications.

## Overview
This directory implements the logic for maintaining a shared trust store that combines system root certificates with custom CAs from WeftLocalDNSReflectors. It ensures that workloads can verify TLS connections to public hostnames resolved internally, avoiding latency from hairpinning traffic through bastion hosts. The package handles both the lifecycle of the ConfigMap containing these certificates and the runtime injection of these certificates into pod specs via volume mounts.

## Deep Dive
### Bundle Construction
The `Build` function aggregates CA data from active reflectors and merges it with local system roots (`/etc/ssl/certs/ca-certificates.crt`). It enforces a minimum count of system roots to prevent publishing broken bundles if the source file is truncated. The resulting bundle is deterministic, sorting reflector CAs by name to ensure consistent ordering.

### ConfigMap Management
`EnsureConfigMap` creates or updates the `weft-trust-bundle` ConfigMap in the target namespace. It sets owner references to the specific reflectors contributing CAs, enabling automatic garbage collection when those reflectors are deleted. A safety check, `CreateConfigMapIfMissing`, prevents race conditions during concurrent reconciler runs.

### Pod Injection
The `InjectPod` function mutates pod specifications to mount the trust bundle ConfigMap over standard certificate paths (e.g., `/etc/ssl/certs/ca-certificates.crt`). It supports both init and regular containers, checking for existing mounts to avoid conflicts. Injection respects opt-out annotations (`weft.aquaduct.dev/inject-trust=false`) and skips Windows nodes where Linux paths do not apply. Fingerprints of injected CAs are recorded in pod annotations for verification.

## Key Interfaces
- `Build`: Assembles the combined trust bundle from system roots and reflector CAs.
- `Source`: Aggregates CA data from cluster reflectors and system files.
- `EnsureConfigMap`: Creates or updates the trust bundle ConfigMap with proper ownership.
- `InjectPod`: Mutates a pod spec to mount the trust bundle over system certificate paths.
- `OptedOut`: Checks if a pod has explicitly opted out of trust injection.
- `TrustsCA`: Verifies if a specific CA fingerprint was injected into a pod.

## Dependencies
- Kubernetes API types (`core/v1`, `meta/v1`)
- Controller-runtime utilities
- Internal PKI operations (`//internal/pki`)

## Subcomponents
- `bundle.go`: Logic for assembling and managing the trust bundle ConfigMap.
- `inject.go`: Logic for injecting trust bundles into Kubernetes pod specs.
- `trust_test.go`: Unit tests for bundle construction and injection logic.