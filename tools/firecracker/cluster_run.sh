#!/usr/bin/env bash
# cluster_run.sh — boot N real firecracker microVMs on a shared host bridge.
#
# The foundation of the "No stub tests" multi-VM rigs: it creates a bridge +
# one user-owned TAP per node (so firecracker opens them without
# CAP_NET_ADMIN), then boots each node through a firecracker_microvm runner
# with the networking env the amended runner.sh.tpl understands
# (FC_TAP / FC_GUEST_MAC / FC_CMDLINE_EXTRA). The guest IP is set by the
# kernel's CONFIG_IP_PNP from the `ip=` cmdline this script passes.
#
# Must run unsandboxed (host network namespace) with passwordless sudo for
# the bridge/TAP setup — i.e. as a `local` bazel test or directly.
#
# Usage:
#   cluster_run.sh --runner PATH --bridge NAME --subnet-gw IP/CIDR \
#                  --timeout SECS --logdir DIR \
#                  --node IDX:IP:MAC:EXTRA_CMDLINE   (repeatable)
#
# Each --node boots one VM; EXTRA_CMDLINE is appended after the ip= arg
# (e.g. "ofd.role=server ofd.peer=10.211.55.10"). The script waits for all
# VMs to exit (or the timeout), leaves per-node logs in LOGDIR/vm<IDX>.log,
# and tears down every TAP + the bridge on exit.
set -uo pipefail

RUNNER="" BRIDGE="ofdbr0" GW="10.211.55.1/24" TIMEOUT="120" LOGDIR="" PERSIST_DIR="" BOOT_DISK_DIR=""
NODES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --runner)      RUNNER="$2"; shift 2 ;;
    --bridge)      BRIDGE="$2"; shift 2 ;;
    --subnet-gw)   GW="$2"; shift 2 ;;
    --timeout)     TIMEOUT="$2"; shift 2 ;;
    --logdir)      LOGDIR="$2"; shift 2 ;;
    --node)        NODES+=("$2"); shift 2 ;;
    # --persist-dir DIR: give each node a persistent /dev/vdb data disk at
    # DIR/persist-<IDX>.img (created + mkfs.ext4'd once, reused across boots
    # so identity + k3s state survive an IP renumber — §3).
    --persist-dir) PERSIST_DIR="$2"; shift 2 ;;
    # --boot-disk-dir DIR: boot each node from a pre-installed GPT disk at
    # DIR/installed-<IDX>.img (root=/dev/vda2) instead of the runner's baked
    # rootfs — the §28 install-to-disk path.
    --boot-disk-dir) BOOT_DISK_DIR="$2"; shift 2 ;;
    *) echo "cluster_run: unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$PERSIST_DIR" ] && mkdir -p "$PERSIST_DIR"
[ -n "$RUNNER" ] || { echo "cluster_run: --runner required" >&2; exit 2; }
[ -n "$LOGDIR" ] || LOGDIR="$(mktemp -d -t fc-cluster.XXXXXX)"
mkdir -p "$LOGDIR"
GW_IP="${GW%/*}"
MASK_CIDR="${GW#*/}"
# Convert CIDR to dotted netmask for the ip= cmdline (kernel accepts both,
# but dotted is the portable form).
cidr_to_mask() { local c="$1" m=""; for i in 1 2 3 4; do if [ "$c" -ge 8 ]; then m+="255"; c=$((c-8)); else m+=$((256 - 2**(8-c) )); c=0; fi; [ "$i" -lt 4 ] && m+="."; done; echo "$m"; }
MASK="$(cidr_to_mask "$MASK_CIDR")"

TAPS=()
cleanup() {
  for t in "${TAPS[@]:-}"; do sudo -n ip link del "$t" 2>/dev/null; done
  sudo -n ip link del "$BRIDGE" 2>/dev/null
}
trap cleanup EXIT

# Fresh bridge.
sudo -n ip link del "$BRIDGE" 2>/dev/null
sudo -n ip link add "$BRIDGE" type bridge || { echo "cluster_run: bridge add failed (need sudo)" >&2; exit 1; }
sudo -n ip addr add "$GW" dev "$BRIDGE"
sudo -n ip link set "$BRIDGE" up

PIDS=()
for spec in "${NODES[@]}"; do
  IFS=, read -r IDX IP MAC EXTRA <<EOF
$spec
EOF
  TAP="fctap${IDX}"
  TAPS+=("$TAP")
  sudo -n ip link del "$TAP" 2>/dev/null
  sudo -n ip tuntap add dev "$TAP" mode tap user "$USER"
  sudo -n ip link set "$TAP" master "$BRIDGE"
  sudo -n ip link set "$TAP" up

  PERSIST_ENV=()
  if [ -n "$PERSIST_DIR" ]; then
    DISK="$PERSIST_DIR/persist-${IDX}.img"
    if [ ! -f "$DISK" ]; then
      truncate -s 2G "$DISK"
      mkfs.ext4 -q -F "$DISK" >/dev/null 2>&1 || echo "cluster_run: mkfs $DISK failed" >&2
    fi
    PERSIST_ENV=("FC_PERSIST_DISK=$DISK")
  fi
  if [ -n "$BOOT_DISK_DIR" ]; then
    PERSIST_ENV+=("FC_BOOT_DISK=$BOOT_DISK_DIR/installed-${IDX}.img" "FC_ROOT_PART=/dev/vda2")
  fi

  env "${PERSIST_ENV[@]}" FC_TAP="$TAP" FC_GUEST_MAC="$MAC" \
    FC_CMDLINE_EXTRA="ip=${IP}::${GW_IP}:${MASK}::eth0:off ${EXTRA}" \
    FC_TIMEOUT="$TIMEOUT" "$RUNNER" >"$LOGDIR/vm${IDX}.log" 2>&1 &
  PIDS+=("$!")
done

for p in "${PIDS[@]}"; do wait "$p"; done
echo "cluster_run: all VMs exited; logs in $LOGDIR"
