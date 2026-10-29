<!-- docdaemon ts:2026-10-29T20:36:52Z hash:93119aac5bc04b05 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script converts an OCI image layout into a bootable ext4 rootfs for Firecracker microVMs.

It flattens image layers while respecting AUFS whiteouts, installs a static busybox, and generates a custom `/sbin/init` that mounts standard kernel filesystems (`/proc`, `/sys`, `/dev`) and executes the container entrypoint. The init script handles environment variable merging from `--extra-env` arguments and ensures clean shutdown via `poweroff`.

The tool requires `mkfs.ext4`, `python3`, and a statically-linked busybox binary. It validates inputs, resolves manifest digests to layer blobs using Python JSON parsing, and applies each layer to a staging directory before merging them into the final rootfs tree. Finally, it creates a sparse ext4 image file seeded with the prepared rootfs content.