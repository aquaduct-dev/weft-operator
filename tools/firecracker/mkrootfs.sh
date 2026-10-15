#!/usr/bin/env bash
# mkrootfs.sh — build an ext4 disk image from a tree of files using
# `mkfs.ext4 -d <staging>` (e2fsprogs >=1.43, no root needed).
#
# Args:
#   --out PATH               Output ext4 image.
#   --size-mb N              Image size in megabytes.
#   --label STR              Filesystem label (max 16 chars).
# Repeatable:
#   --file SRC=DST           Copy SRC to DST in the rootfs at 0644.
#   --exec SRC=DST           Copy SRC to DST in the rootfs at 0755.

set -euo pipefail

# bazel's sandbox starts with a minimal PATH that omits /usr/sbin where
# mkfs.ext4 lives on Debian/Ubuntu. Prepend it so command -v finds it.
export PATH="/usr/sbin:/sbin:${PATH:-/usr/bin:/bin}"

OUT=""
SIZE_MB=""
LABEL=""
FILES=()
EXECS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --out)     OUT="$2";     shift 2 ;;
    --size-mb) SIZE_MB="$2"; shift 2 ;;
    --label)   LABEL="$2";   shift 2 ;;
    --file)    FILES+=("$2"); shift 2 ;;
    --exec)    EXECS+=("$2"); shift 2 ;;
    *) echo "mkrootfs: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[ -n "$OUT" ]     || { echo "mkrootfs: --out required" >&2; exit 2; }
[ -n "$SIZE_MB" ] || { echo "mkrootfs: --size-mb required" >&2; exit 2; }
[ -n "$LABEL" ]   || { echo "mkrootfs: --label required" >&2; exit 2; }

command -v mkfs.ext4 >/dev/null || { echo "mkrootfs: mkfs.ext4 not found (apt install e2fsprogs)" >&2; exit 1; }

WORK="$(mktemp -d -t mkrootfs.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/staging"
mkdir -p "$ROOT"

for spec in "${FILES[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"
  dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkrootfs: dst must be absolute: $spec" >&2; exit 2 ;; esac
  install -D -m 0644 "$src" "$ROOT$dst"
done
for spec in "${EXECS[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"
  dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkrootfs: dst must be absolute: $spec" >&2; exit 2 ;; esac
  install -D -m 0755 "$src" "$ROOT$dst"
done

# Sparse zero-fill, then format. `-F` to overwrite, `-L` for label,
# `-d` to seed contents from the staging tree.
truncate -s "${SIZE_MB}M" "$OUT"
mkfs.ext4 -F -q -L "$LABEL" -d "$ROOT" "$OUT" >/dev/null
