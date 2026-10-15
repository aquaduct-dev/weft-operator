#!/usr/bin/env bash
# mkk3srootfs.sh — build an ext4 rootfs that runs a single-node k3s
# cluster inside firecracker, executes a guest-side test binary against
# 127.0.0.1:6443, and prints its report to /dev/console so the host
# launcher can scrape it.
#
# The k3s binary is laid down at /usr/local/bin/k3s (statically linked
# Go binary; self-extracts containerd-shim-runc-v2, runc, etc. into
# /var/lib/rancher/k3s/data/ at first run). NO container runtime is
# nested — k3s itself is the kernel-level process that manages cgroups,
# starts kubelet, runs containerd, etc. The "directly on the VM image"
# property is preserved.
#
# Layout produced inside the rootfs:
#   /bin/busybox + symlinks for every applet
#   /sbin/busybox -> ../bin/busybox + sbin applet symlinks
#   /sbin/init                — entry point, starts k3s + runs test_binary
#   /usr/local/bin/k3s        — the static k3s binary (~66 MiB)
#   /usr/local/bin/kubectl    — symlink to k3s
#   /usr/local/bin/<test>     — the guest-side test binary
#   /etc/manifests/*.yaml     — extra_manifests, applied after k3s ready
#   /etc/images/*.tar         — extra_images, k3s ctr-imported before tests
#   /etc/scenario             — file containing the scenario name passed
#                               to the test binary as --scenario=<name>
#
# Args:
#   --out PATH               Output ext4 image.
#   --busybox PATH           Statically-linked busybox.
#   --k3s PATH               Statically-linked k3s binary.
#   --init PATH              Source of /sbin/init (PID 1) — the macro's
#                            generated boot script.
#   --test-binary PATH       Statically-linked guest-side test binary.
#   --test-binary-name STR   Basename for /usr/local/bin/<name>.
#   --scenario STR           Scenario name baked into /etc/scenario.
#   --size-mb N              Image size in MB. Must accommodate k3s
#                            (~70 MiB) + busybox + test + scratch space
#                            for k3s self-extraction (~150 MiB) + room
#                            for pulled images. 2048 is a safe default.
#   --label STR              Filesystem label (max 16 chars).
# Repeatable:
#   --file SRC=DST           Copy SRC to DST in the rootfs at 0644.
#   --exec SRC=DST           Copy SRC to DST in the rootfs at 0755.
#   --manifest SRC=DST       Copy SRC to /etc/manifests/<DST>.
#   --image SRC=DST          Copy SRC to /etc/images/<DST> (OCI tarball
#                            consumed by `k3s ctr images import`).
set -euo pipefail

export PATH="/usr/sbin:/sbin:${PATH:-/usr/bin:/bin}"

OUT=""
BUSYBOX=""
K3S=""
INIT=""
TEST_BINARY=""
TEST_BINARY_NAME=""
SCENARIO=""
SIZE_MB=""
LABEL=""
FILES=()
EXECS=()
MANIFESTS=()
IMAGES=()

while [ $# -gt 0 ]; do
  case "$1" in
    --out)              OUT="$2";              shift 2 ;;
    --busybox)          BUSYBOX="$2";          shift 2 ;;
    --k3s)              K3S="$2";              shift 2 ;;
    --init)             INIT="$2";             shift 2 ;;
    --test-binary)      TEST_BINARY="$2";      shift 2 ;;
    --test-binary-name) TEST_BINARY_NAME="$2"; shift 2 ;;
    --scenario)         SCENARIO="$2";         shift 2 ;;
    --size-mb)          SIZE_MB="$2";          shift 2 ;;
    --label)            LABEL="$2";            shift 2 ;;
    --file)             FILES+=("$2");         shift 2 ;;
    --exec)             EXECS+=("$2");         shift 2 ;;
    --manifest)         MANIFESTS+=("$2");     shift 2 ;;
    --image)            IMAGES+=("$2");        shift 2 ;;
    *) echo "mkk3srootfs: unknown arg: $1" >&2; exit 2 ;;
  esac
done

for v in OUT BUSYBOX K3S INIT TEST_BINARY TEST_BINARY_NAME SCENARIO SIZE_MB LABEL; do
  eval "val=\${$v:-}"
  [ -n "$val" ] || { echo "mkk3srootfs: --${v,,} required" >&2; exit 2; }
done

command -v mkfs.ext4 >/dev/null || { echo "mkk3srootfs: mkfs.ext4 not found (apt install e2fsprogs)" >&2; exit 1; }

if ! file -b "$BUSYBOX" | grep -q 'statically linked'; then
  echo "mkk3srootfs: busybox is not statically linked: $BUSYBOX" >&2
  exit 1
fi
if ! file -b "$K3S" | grep -q 'statically linked'; then
  echo "mkk3srootfs: k3s is not statically linked: $K3S" >&2
  exit 1
fi
if file -b "$TEST_BINARY" | grep -q '^ELF' && ! file -b "$TEST_BINARY" | grep -q 'statically linked'; then
  echo "mkk3srootfs: test binary is dynamically linked; rootfs has no libc:" >&2
  echo "  $TEST_BINARY" >&2
  exit 1
fi

WORK="$(mktemp -d -t mkk3srootfs.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/staging"

mkdir -p "$ROOT"/{bin,sbin,etc,etc/manifests,etc/images,dev,proc,sys,tmp,run,var,var/log,var/lib,var/lib/rancher,usr,usr/local,usr/local/bin}
chmod 1777 "$ROOT/tmp"

# Busybox applets — same idea as mkinitramfs.sh, but writing into an
# ext4 rootfs instead of a cpio.
install -m 0755 "$BUSYBOX" "$ROOT/bin/busybox"
for applet in $("$BUSYBOX" --list); do
  [ "$applet" = "busybox" ] && continue
  ln -sf busybox "$ROOT/bin/$applet"
done
for sbin_applet in mount umount sh poweroff reboot ifconfig ip; do
  ln -sf ../bin/busybox "$ROOT/sbin/$sbin_applet"
done

# k3s + kubectl/crictl/ctr aliases (the k3s binary checks argv[0] to
# switch its embedded subcommand mode).
install -m 0755 "$K3S" "$ROOT/usr/local/bin/k3s"
for tool in kubectl crictl ctr; do
  ln -sf k3s "$ROOT/usr/local/bin/$tool"
done

install -D -m 0755 "$INIT"        "$ROOT/sbin/init"
install -D -m 0755 "$TEST_BINARY" "$ROOT/usr/local/bin/$TEST_BINARY_NAME"
printf '%s\n' "$SCENARIO"         > "$ROOT/etc/scenario"
printf '%s\n' "$TEST_BINARY_NAME" > "$ROOT/etc/test_binary"

cat >"$ROOT/etc/fstab" <<'EOF'
proc      /proc       proc      defaults  0 0
sysfs     /sys        sysfs     defaults  0 0
devtmpfs  /dev        devtmpfs  defaults  0 0
tmpfs     /tmp        tmpfs     defaults  0 0
tmpfs     /run        tmpfs     defaults  0 0
EOF

# Empty resolv.conf — k3s' embedded CoreDNS reads from /etc/resolv.conf
# at start; an absent file would error.
: >"$ROOT/etc/resolv.conf"
echo "localhost" >"$ROOT/etc/hostname"
cat >"$ROOT/etc/hosts" <<'EOF'
127.0.0.1   localhost server0
::1         localhost
EOF

# Minimal passwd/group so kubelet's setuid lookups (when present) don't
# error. k3s embedded kubelet usually runs as root.
cat >"$ROOT/etc/passwd" <<'EOF'
root:x:0:0:root:/root:/bin/sh
EOF
cat >"$ROOT/etc/group" <<'EOF'
root:x:0:
EOF

for spec in "${FILES[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"; dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkk3srootfs: --file dst must be absolute: $spec" >&2; exit 2 ;; esac
  install -D -m 0644 "$src" "$ROOT$dst"
done
for spec in "${EXECS[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"; dst="${spec#*=}"
  case "$dst" in /*) ;; *) echo "mkk3srootfs: --exec dst must be absolute: $spec" >&2; exit 2 ;; esac
  install -D -m 0755 "$src" "$ROOT$dst"
done
for spec in "${MANIFESTS[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"; dst="${spec#*=}"
  case "$dst" in */*) echo "mkk3srootfs: --manifest dst must be a basename: $spec" >&2; exit 2 ;; esac
  install -D -m 0644 "$src" "$ROOT/etc/manifests/$dst"
done
for spec in "${IMAGES[@]:-}"; do
  [ -z "$spec" ] && continue
  src="${spec%%=*}"; dst="${spec#*=}"
  case "$dst" in */*) echo "mkk3srootfs: --image dst must be a basename: $spec" >&2; exit 2 ;; esac
  install -D -m 0644 "$src" "$ROOT/etc/images/$dst"
done

truncate -s "${SIZE_MB}M" "$OUT"
mkfs.ext4 -F -q -L "$LABEL" -d "$ROOT" "$OUT" >/dev/null
