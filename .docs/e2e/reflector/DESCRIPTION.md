<!-- docdaemon ts:2026-10-30T03:01:10Z hash:228721fd94847337 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the end-to-end test suite for the WeftLocalDNSReflector operator, validating DNS reflection, TLS trust injection, and in-cluster traffic routing within an isolated k3s environment.

## Overview
The tests verify that the reflector correctly manages hostname claims, injects certificates into node trust stores, and ensures client workloads can resolve and connect to reflected services without external network access. It supports multiple scenarios including default cluster-wide reflection, complex claim handovers, and full lifecycle management of proxy failures and certificate reissuance.

## Deep Dive
### Test Execution Environment
Tests run inside a firecracker microVM hosting a single-node k3s cluster. The `BUILD.bazel` target compiles a static Go binary (`reflectore2e`) and packages it into a preloaded image, ensuring an airgapped setup where no external image pulls occur. Helm values in `e2e-values.yaml` enforce this offline constraint by setting `image.pullPolicy: Never`.

### Scenario Orchestration
The main binary acts as a multi-mode agent. In **Default** mode, it executes steps defined in `default_scenario.go` or `scenario.go`, verifying operator installation, webhook registration, and basic DNS resolution. The **Claims** scenario (`claims_scenario.go`) tests conflict resolution between reflectors with different scopes and ages, ensuring older reflectors win and trust gates behave correctly when untrusted pods are present. The **Lifecycle** scenario (`lifecycle_scenario.go`) validates robustness against proxy failures, certificate reissuance, and manual state cleanup.

### Client Validation
The `client.go` module simulates real workload behavior. It performs DNS lookups, issues HTTPS GET requests using only the system trust store (verifying the trust webhook’s effectiveness), and pushes OCI images to the reflected registry. Helper functions in `extras.go` and `kube.go` manage pod lifecycles, poll for status conditions, and assert on node-level artifacts like `/etc/hosts` entries and CA bundle fingerprints.

## Key Interfaces
- `runClient`: Entry point for client workload validation, handling DNS and TLS checks.
- `claimsSteps`: Returns the sequence of steps for testing hostname claim conflicts.
- `defaultReflectorSteps`: Orchestrates validation of the automatic cluster-default reflector.
- `lifecycleSteps`: Manages tests for certificate reissuance, proxy down events, and state restoration.
- `runFetch`: Executes HTTP GET requests to verify registry connectivity via reflected routes.
- `waitFor`: Generic polling loop for Kubernetes resource state changes.

## Dependencies
- `k3s`: Single-node Kubernetes distribution running in the firecracker VM.
- `Helm`: Used to render operator manifests with specific test configurations.
- `rules_firecracker`: Bazel rules for managing the microVM environment.
- `e2ekit`: Shared testing framework for guest harness logic.

## Subcomponents
- `00-namespaces.yaml`: Defines isolated namespaces for operator, backend, and client.
- `BUILD.bazel`: Bazel build targets for the e2e binary and preloaded images.
- `claims_scenario.go`: Tests for hostname ownership conflicts and trust gate logic.
- `client.go`: Workload simulation for DNS resolution and image pushing.
- `default_scenario.go`: Tests for the automatic cluster-default reflector lifecycle.
- `e2e-values.yaml`: Helm values for airgapped, offline test execution.
- `extras.go`: Shared helpers for reflector operations and node trust assertions.
- `fetch.go`: HTTP fetch mode for verifying registry route connectivity.
- `kube.go`: Kubernetes utilities for pod management and log retrieval.
- `lifecycle_scenario.go`: Tests for proxy failures and certificate reissuance.
- `main.go`: Multi-mode entry point for the e2e test binary.
- `no-default-reflector.yaml`: Configuration to disable the default reflector.
- `scenario.go`: Core scenario for basic DNS reflection and trust injection.