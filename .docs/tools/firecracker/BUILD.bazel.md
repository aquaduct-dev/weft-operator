<!-- docdaemon ts:2026-10-29T20:36:24Z hash:91d80446c9390a48 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the build targets for Firecracker microVMs, including kernel compilation, rootfs generation, and e2e test infrastructure for k3s clusters.

It imports `rules_firecracker` macros via `defs.bzl` to construct hermetic VM environments. The `vmlinux` target builds a Linux 6.1 kernel locally with netfilter extensions required for kube-proxy connectivity. Static binaries like `busybox_static` and `k3s_binary` are wrapped in genrules to ensure executable permissions.

Helper scripts (`mkinitramfs`, `mkrootfs`, `mkk3srootfs`) generate filesystem images. `nettest_vm` provides a basic networking self-test, while `k3s_cluster_vm` boots a multi-node k3s cluster over a shared host bridge to validate end-to-end network interface support without stubs.