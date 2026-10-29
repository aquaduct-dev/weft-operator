<!-- docdaemon ts:2026-10-29T20:36:49Z hash:4d92d5b9a608bd34 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This Bazel build file defines the Go library, static binary, and Firecracker-based integration test for the k3s smoke test suite.

The `k3s_smoke_lib` target compiles `main.go` into a private Go library. It is embedded into the `k3s_smoke` binary via `go_binary`. This binary is statically linked by default because it runs inside a minimal k3s rootfs containing only busybox and the k3s binary, with no external libc dependencies.

The `firecracker_k3s_test` target orchestrates an end-to-end test named `k3s_smoke`. It boots a single-node k3s server within a Firecracker microVM and executes the `k3s_smoke` binary to verify that the guest can read the kubeconfig and that `kubectl get nodes` reports a Ready node. The test is tagged `requires-kvm`, allowing hosts without KVM support to skip execution via `--test_tag_filters=-requires-kvm`.