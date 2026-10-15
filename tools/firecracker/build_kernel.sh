#!/bin/sh
# Build a firecracker-compatible uncompressed vmlinux ELF from a pinned
# linux source tarball + .config. Intended to be driven from a bazel
# genrule with `local = True`; uses the host toolchain (gcc, make, bc,
# flex, bison, libelf, libssl). The pinned .config disables CONFIG_MODULES
# and CONFIG_DEBUG_INFO_BTF so no module-signing keys or pahole are needed.
set -eu

usage() {
    echo "Usage: $0 --tarball PATH --config PATH --out PATH [--jobs N]" >&2
    exit 2
}

JOBS=$(nproc 2>/dev/null || echo 4)
TARBALL=
CONFIG=
OUT=

while [ $# -gt 0 ]; do
    case "$1" in
        --tarball) TARBALL=$2; shift 2;;
        --config) CONFIG=$2; shift 2;;
        --out) OUT=$2; shift 2;;
        --jobs) JOBS=$2; shift 2;;
        *) usage;;
    esac
done

[ -n "$TARBALL" ] && [ -n "$CONFIG" ] && [ -n "$OUT" ] || usage

# Resolve to absolute paths before chdir.
TARBALL=$(readlink -f "$TARBALL")
CONFIG=$(readlink -f "$CONFIG")
OUT_ABS=$(readlink -f "$(dirname "$OUT")")/$(basename "$OUT")

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

tar -C "$WORK" -xf "$TARBALL"
# Tarball extracts to a single linux-<version>/ dir.
SRC=$(ls -d "$WORK"/linux-*/ | head -1)
cd "$SRC"

cp "$CONFIG" .config
# Apply config + fill defaults for any options the pinned config doesn't
# mention (e.g. when source version drifts from config target version).
make olddefconfig >/dev/null

# `vmlinux` is the uncompressed ELF firecracker boots directly.
make "-j${JOBS}" vmlinux

cp vmlinux "$OUT_ABS"
