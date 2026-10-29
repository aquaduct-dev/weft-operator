<!-- docdaemon ts:2026-10-29T20:36:53Z hash:5fc3ea3264bfc38e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script builds an ext4 disk image from a staging directory of files, suitable for use as a root filesystem in Firecracker microVMs.

It accepts command-line arguments to specify the output path, image size, and filesystem label. It supports two modes for populating the image: `--file` copies regular files with `0644` permissions, while `--exec` copies binaries or scripts with `0755` permissions. The script ensures `mkfs.ext4` is available by prepending `/usr/sbin` to the PATH, handling environments like Bazel sandboxes where this path might be missing.

A temporary staging directory is created to hold the file tree. All specified source files are installed into this staging area at their designated absolute destination paths. Once the staging tree is populated, the script creates a sparse zero-filled file of the requested size using `truncate`. Finally, it formats this file as an ext4 filesystem using `mkfs.ext4 -d`, seeding the contents directly from the staging directory. A cleanup trap ensures the temporary staging directory is removed upon exit.