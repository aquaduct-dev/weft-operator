<!-- docdaemon ts:2026-10-30T03:01:07Z hash:94af8896f89137a8 model:openrouter/qwen/qwen3.7-flash -->
# e2e/trust

## TL;DR
This directory contains the end-to-end test suite for the trust-injection webhook, validating pod mutation logic and CA bundle reflection in a live k3s cluster.

## Overview
The tests verify that the `weft-operator` correctly injects trust bundles into workloads via a mutating webhook. It ensures injection only occurs when a reflector is ready, respects namespace isolation, and handles edge cases like opt-outs and dry runs. The suite runs inside a firecracker microVM to simulate an airgapped environment with a real system trust store.

## Deep Dive
### Test Execution
The `webhook.go` file defines the core scenario, orchestrating operator installation and step-by-step validation against the API server. It checks that pods receive annotations and read-only volume mounts (0444) at standard system paths only after a `WeftLocalDNSReflector` becomes ready.

### Network Namespace Metrics
To verify operator behavior without external log aggregation, `netns.go` uses Linux system calls to enter the operator’s network namespace. This allows the test runner to dial `127.0.0.1:8080/metrics` directly, parsing request counters for the trust webhook endpoint.

### Environment Setup
`00-namespaces.yaml` provisions isolated namespaces (`e2e-trust`, `e2e-foreign`) to test boundary enforcement. It includes a pre-existing ConfigMap in the foreign namespace to ensure the reflector ignores unrelated resources and respects ownership boundaries.

## Key Interfaces
*   `operatorMetrics`: Retrieves raw metrics text from the operator pod by switching network namespaces.
*   `webhookRequests`: Parses metrics to calculate total requests to the `/inject-trust` webhook path.
*   `webhookScenario`: Orchestrates the sequence of steps for the trust injection end-to-end test.

## Dependencies
*   Kubernetes API types for resource manipulation.
*   Internal PKI utilities for certificate handling.
*   E2E kit helpers for test execution and assertion.
*   Bazel build rules for container image generation.

## Subcomponents
*   `00-namespaces.yaml`: Kubernetes manifests for test namespaces and initial ConfigMaps.
*   `BUILD.bazel`: Bazel targets for the test binary and Alpine container image.
*   `main.go`: Entry point supporting sleep mode and test execution.
*   `netns.go`: Utilities for accessing operator metrics via network namespace switching.
*   `webhook.go`: Implementation of the webhook mutation and injection test scenarios.