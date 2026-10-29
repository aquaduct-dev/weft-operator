<!-- docdaemon ts:2026-10-29T20:37:08Z hash:df6d30d92a282b6b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements a static Go binary and Bazel integration test to verify k3s cluster readiness inside a Firecracker microVM.

## Overview
The `k3s_smoke` package provides an end-to-end validation tool for ensuring that a single-node k3s server is fully bootstrapped within a minimal rootfs environment. It serves as the guest-side agent for the `firecracker_k3s_test`, which orchestrates the microVM lifecycle using Bazel. The primary goal is to confirm that the cluster can accept workloads by verifying kubeconfig availability and node readiness.

## Deep Dive
### Guest-Side Validation Logic
The `main.go` binary executes a strict two-step verification process. First, it checks for the existence of `/etc/rancher/k3s/k3s.yaml`. If this file is absent, the test fails immediately, indicating a bootstrap error. Second, it polls for node readiness using `k3s kubectl get nodes`. This polling loop continues until a node reports a `Ready` status or a timeout expires. To maintain compatibility with the minimal busybox-based rootfs, the binary avoids heavy JSON parsing libraries, instead using line-by-line JSONPath output inspection to detect the `True` status condition.

### Build and Test Orchestration
The `BUILD.bazel` file defines three key targets. `k3s_smoke_lib` compiles the Go source into a private library. `k3s_smoke` builds this into a statically linked binary, eliminating external libc dependencies required by the sparse guest environment. Finally, `firecracker_k3s_test` runs the integration test, tagging it with `requires-kvm` to allow hosts without KVM support to skip execution. This setup ensures that smoke tests only run in environments capable of supporting hardware virtualization.

## Key Interfaces
*   `reportPC`: Struct defining the schema for the lib/clusterharness report format.
*   `waitNodeReady`: Function implementing the polling loop with fixed timeout and backoff.
*   `hasReady`: Helper function parsing JSONPath output to detect Ready node status.

## Dependencies
*   Bazel (for build and test orchestration)
*   Firecracker (for microVM management)
*   k3s (for cluster operations and kubectl)
*   Busybox (as the minimal rootfs base)

## Subcomponents
*   `BUILD.bazel`: Bazel build definitions for library, binary, and test targets.
*   `main.go`: Guest-side validation logic and reporting implementation.