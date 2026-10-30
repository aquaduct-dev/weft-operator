<!-- docdaemon ts:2026-10-30T03:01:07Z hash:08ea740a9c4e9e5d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `trust` package manages the assembly of CA bundles and their injection into Kubernetes pods to ensure workload trust without image modifications.

## Overview
This directory implements the logic for maintaining a shared trust store within the cluster. It aggregates system root certificates with custom CAs from DNS reflectors, publishes them as a ConfigMap, and uses a webhook to mount these certificates into pod volumes, overriding standard system paths.

## Deep Dive
### Bundle Construction
The `Source` type aggregates CA data from active cluster reflectors and reads local system roots. The `Build` function merges these sources, enforcing a minimum threshold of 20 system certificates to prevent TLS breakage from truncated bundles. Output is deterministic, sorted by reflector name.

### ConfigMap Management
Trust bundles are stored in a `weft-trust-bundle` ConfigMap. `EnsureConfigMap` creates or updates this resource with managed labels for garbage collection. `CheckConfigMap` prevents accidental overwrites of external stores, while `CreateConfigMapIfMissing` handles race conditions during initialization. Owner references link the ConfigMap to active reflectors for cleanup.

### Pod Injection
The `InjectPod` function mutates pod specs to mount the trust bundle ConfigMap over standard certificate paths (e.g., `/etc/ssl/certs/ca-certificates.crt`). It supports both init and regular containers, ensuring idempotency by checking for existing mounts. Injection respects opt-out annotations, skips Windows nodes, and avoids conflicts with root filesystem mounts. Fingerprints of injected CAs are recorded in pod annotations for verification.

## Key Interfaces
- `Bundle`: Holds combined CA data, reflector-only CAs, and sorted fingerprints.
- `Build`: Assembles the final trust bundle from system roots and reflector CAs.
- `Source`: Aggregates CA data from cluster reflectors and disk-based system roots.
- `EnsureConfigMap`: Creates or updates the trust bundle ConfigMap in the cluster.
- `InjectPod`: Mutates a pod spec to mount the trust bundle volume.
- `OptedOut`: Checks if a pod has explicit opt-out annotations.
- `TrustsCA`: Verifies if a specific CA fingerprint was injected into a pod.

## Dependencies
- Kubernetes API types (`core/v1`, `meta/v1`)
- Controller-runtime utilities
- Internal PKI package (`//internal/pki`)

## Subcomponents
- `bundle.go`: Trust bundle assembly and ConfigMap management.
- `inject.go`: Pod mutation logic for CA injection.
- `trust_test.go`: Unit tests for bundle construction and injection.