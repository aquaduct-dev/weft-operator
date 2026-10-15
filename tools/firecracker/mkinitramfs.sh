#!/usr/bin/env bash
# mkinitramfs.sh — pack a cpio+gzip initramfs from a set of files.
#
# Args (all required unless marked):
#   --out PATH               Output cpio.gz file.
#   --busybox PATH           Statically-linked busybox; symlinked into
#                            /bin for every applet plus /sbin/{init,
#                            mount,umount,sh}.
#   --init PATH              Source of /init (PID 1). Installed 0755.
# Repeatable:
#   --file SRC=DST           Copy SRC to DST inside the cpio at 0644.
#   --exec SRC=DST           Copy SRC to DST inside the cpio at 0755.
#                            ELF entries are checked for static linkage
#                            (initramfs has no libc).

set -euo pipefail

OUT=""
BUSYBOX=""
INIT=""
FILES=()
EXECS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --out)     OUT="$2";     shift 2 ;;
    --busybox) BUSYBOX="$2"; shift 2 ;;
    --init)    INIT="$2";    shift 2 ;;
    --file)    FILES+=("$2"); shift 2 ;;
    --exec)    EXECS+=("$2"); shift 2 ;;
    *) echo "mkinitramfs: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[ -n "$OUT" ]     || { echo "mkinitramfs: --out required" >&2; exit 2; }
[ -n "$BUSYBOX" ] || { echo "mkinitramfs: --busybox required" >&2; exit 2; }
[ -n "$INIT" ]    || { echo "mkinitramfs: --init required" >&2; exit 2; }

if ! file -b "$BUSYBOX" | grep -q 'statically linked'; then
  echo "mkinitramfs: busybox is not statically linked: $BUSYBOX" >&2
  exit 1
fi

WORK="$(mktemp -d -t mkinitramfs.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/rootfs"
mkdir -p "$ROOT"/{bin,sbin,etc,dev,proc,sys,tmp,run}

install -m 0755 "$BUSYBOX" "$ROOT/bin/busybox"
# Skip "busybox" itself — symlinking busybox→busybox creates ELOOP and
# breaks /init's shebang resolution at boot.
for applet in $("$BUSYBOX" --list); do
  [ "$applet" = "busybox" ] && continue
  ln -sf busybox "$ROOT/bin/$applet"
done
for sbin_applet in init mount umount sh; do
  ln -sf ../bin/busybox "$ROOT/sbin/$sbin_applet"
done

install -D -m 0755 "$INIT" "$ROOT/init"

for spec in "${FILES[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"
  dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkinitramfs: dst must be absolute: $spec" >&2; exit 2 ;; esac
  install -D -m 0644 "$src" "$ROOT$dst"
done

for spec in "${EXECS[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"
  dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkinitramfs: dst must be absolute: $spec" >&2; exit 2 ;; esac
  if file -b "$src" | grep -q '^ELF'; then
    if ! file -b "$src" | grep -q 'statically linked'; then
      echo "mkinitramfs: $src is dynamically linked; initramfs has no libc" >&2
      exit 1
    fi
  fi
  install -D -m 0755 "$src" "$ROOT$dst"
done

cat >"$ROOT/etc/fstab" <<'EOF'
proc      /proc       proc      defaults  0 0
sysfs     /sys        sysfs     defaults  0 0
devtmpfs  /dev        devtmpfs  defaults  0 0
tmpfs     /tmp        tmpfs     defaults  0 0
tmpfs     /run        tmpfs     defaults  0 0
EOF

( cd "$ROOT" && find . -print0 | cpio --null -ov --format=newc 2>/dev/null \
  | gzip -9 ) > "$OUT"
