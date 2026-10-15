#!/usr/bin/env bash
# firecracker_install_cluster_test wrapper (template). Real §28 rig: for each
# node, run the REAL openfactoryd-installer to lay a bootable cluster rootfs
# onto a fresh GPT disk (install-to-disk), then boot every node from its
# installed disk (no installer media) on a shared bridge and assert the
# cluster forms. Runs `local` (host netns + sudo for losetup/TAP + kvm);
# SKIPs cleanly when unavailable.
set -uo pipefail

if [[ -z "${RUNFILES_DIR:-}" ]]; then
  if [[ -d "${BASH_SOURCE[0]}.runfiles" ]]; then RUNFILES_DIR="${BASH_SOURCE[0]}.runfiles"; fi
fi
if [[ -z "${RUNFILES_DIR:-}" && -n "${TEST_SRCDIR:-}" ]]; then RUNFILES_DIR="$TEST_SRCDIR"; fi
export RUNFILES_DIR
# shellcheck disable=SC1091
source "$RUNFILES_DIR/bazel_tools/tools/bash/runfiles/runfiles.bash" \
  || { echo "install-test: cannot source runfiles.bash" >&2; exit 1; }

RUNNER="$(rlocation '@@RUNNER_RLOC@@')"
CLUSTER_RUN="$(rlocation '@@CLUSTER_RUN_RLOC@@')"
INSTALLER="$(rlocation '@@INSTALLER_RLOC@@')"
ROOTFS="$(rlocation '@@ROOTFS_RLOC@@')"
[[ -x "$RUNNER" && -x "$INSTALLER" && -f "$ROOTFS" ]] || { echo "install-test: missing runner/installer/rootfs" >&2; exit 1; }
if [[ ! -e /dev/kvm ]]; then echo "install-test: /dev/kvm absent — SKIP" >&2; exit 0; fi
if ! sudo -n true 2>/dev/null; then echo "install-test: passwordless sudo unavailable — SKIP" >&2; exit 0; fi
for t in losetup sgdisk mkfs.fat mkfs.ext4 mkfs.exfat; do command -v "$t" >/dev/null || { echo "install-test: missing $t — SKIP" >&2; exit 0; }; done

LOG="$(mktemp -d -t fc-install.XXXXXX)"
INST="$LOG/installed"; mkdir -p "$INST"
SECRET='@@SECRET@@'

echo "install-test: PHASE 1 — install each node to disk" >&2
for idx in @@IDXS@@; do
  DISK="$INST/installed-${idx}.img"
  truncate -s 5G "$DISK"
  LOOP="$(sudo -n losetup -fP --show "$DISK")"
  cat > "$LOG/settings-${idx}.ini" <<EOF
[node]
role = server
[cluster]
cluster_secret = ${SECRET}
[install]
target_disk = ${LOOP}
wipe_confirm = yes-i-am-sure
EOF
  sudo -n "$INSTALLER" --settings "$LOG/settings-${idx}.ini" --rootfs-image "$ROOTFS" >"$LOG/install-${idx}.log" 2>&1 \
    || { echo "install-test: installer failed for node $idx" >&2; sudo -n losetup -d "$LOOP" 2>/dev/null; tail -5 "$LOG/install-${idx}.log" >&2; }
  sudo -n losetup -d "$LOOP" 2>/dev/null
done

echo "install-test: PHASE 2 — boot each node from its installed disk" >&2
NODES_RAW='@@NODES@@'
NODE_ARGS=(); OLD_IFS="$IFS"; IFS=';'
for spec in $NODES_RAW; do [[ -n "$spec" ]] && NODE_ARGS+=(--node "$spec"); done
IFS="$OLD_IFS"
bash "$CLUSTER_RUN" --runner "$RUNNER" --bridge "ofdinst$$" --subnet-gw '@@GW@@' \
  --timeout '@@TIMEOUT@@' --logdir "$LOG/boot" --boot-disk-dir "$INST" \
  "${NODE_ARGS[@]}" >"$LOG/orch.log" 2>&1 || true

echo "=== node markers ===" >&2
grep -hE 'OFD_BOOTING|OFD_CLUSTER|Mounted root|Cannot open root' "$LOG"/boot/vm*.log 2>/dev/null | tail -20 >&2 || true
if grep -qhF '@@EXPECT_MARKER@@' "$LOG"/boot/vm*.log 2>/dev/null; then
  echo "install-test: PASS — found '@@EXPECT_MARKER@@'" >&2
  rm -rf "$LOG"; exit 0
fi
echo "install-test: FAIL — '@@EXPECT_MARKER@@' not seen" >&2
exit 1
