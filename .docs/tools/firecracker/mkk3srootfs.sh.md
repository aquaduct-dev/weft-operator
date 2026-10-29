<!-- docdaemon ts:2026-10-29T20:36:50Z hash:d850adfa8c195a7e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script builds a minimal ext4 rootfs image containing a statically-linked k3s single-node cluster, busybox utilities, and a guest-side test binary for Firecracker microVM e2e testing.

It accepts paths to static binaries (busybox, k3s, test binary) and configuration files, validates their linkage status, and stages them into a temporary directory structure. The script creates necessary symlinks for k3s subcommands (kubectl, crictl, ctr) and busybox applets, then generates essential system files like `/etc/fstab`, `/etc/hosts`, and `/etc/passwd` to ensure the embedded kubelet and containerd operate correctly without external dependencies.

Finally, it formats an ext4 image of the specified size using `mkfs.ext4 -d` to populate it directly from the staged directory, ensuring the resulting VM image is self-contained and ready for launch.