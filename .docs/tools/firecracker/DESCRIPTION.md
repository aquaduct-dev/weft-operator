<!-- docdaemon ts:2026-10-29T20:39:02Z hash:7bb4f5145f753899 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory provides the build infrastructure, filesystem generation tools, and integration test harnesses for running k3s clusters inside Firecracker microVMs.

## Overview
It enables end-to-end validation of Kubernetes networking by booting real microVMs with minimal rootfs images. The setup bridges Bazel build rules with host-level network configuration to create isolated but connected VM environments. This allows testing of complex scenarios like in-cluster DNS resolution and multi-node cluster formation without relying on stubs or mock services.

## Deep Dive
### Build Infrastructure
`defs.bzl` defines macros for generating boot assets and launching VMs. `build_kernel.sh` compiles a pinned Linux 6.1 kernel optimized for Firecracker, disabling modules and debug info for speed. Helper scripts (`mkrootfs`, `mkinitramfs`, `mkk3srootfs`) assemble ext4 and cpio images from static binaries, ensuring all dependencies are bundled for hermetic execution.

### Test Orchestration
The directory supports single-node and multi-node cluster tests. `gen_test.sh` and `gen_k3s_test.sh` generate shell wrappers that launch VMs, stream serial logs, and parse JSON reports delimited by specific markers. `cluster_run.sh` manages shared host bridges and TAP interfaces, allowing multiple VMs to communicate over a virtual network while avoiding `CAP_NET_ADMIN` requirements.

### Guest-Side Logic
Scripts like `k3s_init.sh` and `k3s_cluster_init.sh` handle initialization within the VM, configuring dummy networks and starting k3s servers or agents. The `k3s_smoke/` subdirectory contains a Go binary that validates node readiness by polling `kubectl` output, providing a robust check for cluster bootstrap success before proceeding to workload tests.

## Key Interfaces
*   `firecracker_microvm`: Macro to define a microVM target with configurable vCPUs and memory.
*   `firecracker_test`: Wrapper for asserting serial console markers within a timeout.
*   `firecracker_k3s_test`: Provisioner for single-node k3s clusters with guest-side validation.
*   `firecracker_cluster_test`: Orchestrator for multi-VM scenarios on shared bridges.
*   `waitNodeReady`: Function polling for kubelet node readiness status.

## Dependencies
*   Bazel (rules_firecracker, go_rules)
*   Firecracker binary
*   Linux kernel source (pinned 6.1)
*   k3s static binaries
*   Busybox static binary
*   mkfs.ext4, cpio, gzip

## Subcomponents
*   BUILD.bazel
*   defs.bzl
*   build_kernel.sh
*   mkrootfs.sh
*   mkinitramfs.sh
*   mkk3srootfs.sh
*   mkcontainerfs.sh
*   gen_test.sh
*   gen_k3s_test.sh
*   cluster_run.sh
*   nettest_init.sh
*   k3s_init.sh
*   k3s_cluster_init.sh
*   k3s_smoke/