#!/usr/bin/env bash
# Firecracker microVM launcher (template). The firecracker_microvm macro
# substitutes @@SENTINEL@@ tokens at build time. At runtime the script
# resolves kernel/boot artifacts via runfiles, materializes a vm.json,
# and execs firecracker. If FC_TIMEOUT is set, the VM is killed after
# that many seconds and exit 0 is returned (firecracker_test relies on
# this so the boot itself doesn't have to gracefully shut down).

set -uo pipefail

# --- runfiles.bash bootstrap (canonical Bazel boilerplate) ---
if [[ -z "${RUNFILES_DIR:-}" ]]; then
  if [[ -d "${BASH_SOURCE[0]}.runfiles" ]]; then
    RUNFILES_DIR="${BASH_SOURCE[0]}.runfiles"
  elif [[ -d "$0.runfiles" ]]; then
    RUNFILES_DIR="$0.runfiles"
  fi
fi
if [[ -z "${RUNFILES_DIR:-}" && -n "${TEST_SRCDIR:-}" ]]; then
  RUNFILES_DIR="$TEST_SRCDIR"
fi
export RUNFILES_DIR
# shellcheck disable=SC1091
source "$RUNFILES_DIR/bazel_tools/tools/bash/runfiles/runfiles.bash" \
  || { echo "runner: cannot source runfiles.bash" >&2; exit 1; }

KERNEL="$(rlocation '@@KERNEL_RLOC@@')"
BOOT="$(rlocation '@@BOOT_RLOC@@')"
[[ -n "$KERNEL" && -f "$KERNEL" ]] || { echo "runner: kernel not found at @@KERNEL_RLOC@@" >&2; exit 1; }
[[ -n "$BOOT"   && -f "$BOOT"   ]] || { echo "runner: boot artifact not found at @@BOOT_RLOC@@" >&2; exit 1; }

WORK="$(mktemp -d -t fc-microvm.XXXXXX)"
# Tests may want to keep $WORK around (e.g. to inspect a written-to
# extra drive after the VM exits). FC_KEEP_WORK=1 disables the trap;
# callers are then responsible for cleanup.
if [[ -z "${FC_KEEP_WORK:-}" ]]; then
  trap 'rm -rf "$WORK"' EXIT
else
  echo "fc-runner: keeping work dir $WORK (FC_KEEP_WORK=1)" >&2
fi

# Optional extra drive — when @@EXTRA_DRIVE_RLOC@@ is non-empty, the
# microvm gets a writable virtio-block device backed by a copy of
# that file. In initrd-boot mode this is the only drive, so it
# enumerates as /dev/vda inside the guest.
EXTRA_DRIVE_JSON=""
if [[ -n "@@EXTRA_DRIVE_RLOC@@" ]]; then
  EXTRA="$(rlocation '@@EXTRA_DRIVE_RLOC@@')"
  [[ -n "$EXTRA" && -f "$EXTRA" ]] || { echo "runner: extra drive not found at @@EXTRA_DRIVE_RLOC@@" >&2; exit 1; }
  cp "$EXTRA" "$WORK/extra.img"
  chmod 0644 "$WORK/extra.img"
  EXTRA_DRIVE_JSON='{"drive_id":"extra","path_on_host":"extra.img","is_root_device":false,"is_read_only":false}'
fi

# Optional network interface — the amendment that makes every microVM
# multi-VM-capable (CRITERIA "No stub tests"). When FC_TAP names a host
# TAP device (created by the cluster orchestrator and enslaved to a shared
# bridge), the VM gets a virtio-net NIC on it; the guest IP is set via
# FC_CMDLINE_EXTRA (`ip=<ip>::<gw>:<mask>::eth0:off`, honored by the
# kernel's CONFIG_IP_PNP). Empty FC_TAP = no NIC, so single-VM tests are
# byte-for-byte unchanged.
NET_FIELD=""
if [[ -n "${FC_TAP:-}" ]]; then
  NET_FIELD="\"network-interfaces\": [{\"iface_id\": \"eth0\", \"host_dev_name\": \"${FC_TAP}\", \"guest_mac\": \"${FC_GUEST_MAC:-06:00:AC:10:00:02}\"}],"
fi
CMDLINE_EXTRA="${FC_CMDLINE_EXTRA:-}"

# Optional persistent data disk (§3 IP-renumber heal needs identity + k3s
# state to survive a reboot). FC_PERSIST_DISK names a host backing file that
# is attached as a second virtio-block device (/dev/vdb in rootfs mode) by
# ABSOLUTE path — NOT copied into $WORK — so writes persist across boots of
# the same logical node. The orchestrator creates the file once and reuses
# it for both boots.
PERSIST_DRIVE_JSON=""
if [[ -n "${FC_PERSIST_DISK:-}" ]]; then
  [[ -f "$FC_PERSIST_DISK" ]] || { echo "runner: persist disk not found: $FC_PERSIST_DISK" >&2; exit 1; }
  PERSIST_ABS="$(cd "$(dirname "$FC_PERSIST_DISK")" && pwd)/$(basename "$FC_PERSIST_DISK")"
  PERSIST_DRIVE_JSON=",{\"drive_id\": \"persist\", \"path_on_host\": \"${PERSIST_ABS}\", \"is_root_device\": false, \"is_read_only\": false}"
fi

# Symlink artifacts into a clean working dir; firecracker's path
# resolver doesn't love runfiles paths with ../external/... in them.
ln -sf "$KERNEL" "$WORK/vmlinux.bin"

# FC_BOOT_DISK overrides the baked initrd/rootfs: boot directly from a
# pre-partitioned disk (an installed system, §28) by absolute path, with
# root pointed at FC_ROOT_PART (default /dev/vda2, the OFD_ROOTFS partition).
# The kernel needs CONFIG_EFI_PARTITION=y to read the disk's GPT.
if [[ -n "${FC_BOOT_DISK:-}" ]]; then
  [[ -f "$FC_BOOT_DISK" ]] || { echo "runner: boot disk not found: $FC_BOOT_DISK" >&2; exit 1; }
  BOOT_ABS="$(cd "$(dirname "$FC_BOOT_DISK")" && pwd)/$(basename "$FC_BOOT_DISK")"
  ROOT_PART="${FC_ROOT_PART:-/dev/vda2}"
  cat > "$WORK/vm.json" <<JSON
{
  "boot-source": {
    "kernel_image_path": "vmlinux.bin",
    "boot_args": "@@KERNEL_CMDLINE@@ root=${ROOT_PART} ${CMDLINE_EXTRA}"
  },
  ${NET_FIELD}
  "drives": [
    {"drive_id": "boot", "path_on_host": "${BOOT_ABS}", "is_root_device": false, "is_read_only": false}
  ],
  "machine-config": {"vcpu_count": @@VCPUS@@, "mem_size_mib": @@MEM_MIB@@}
}
JSON
  cd "$WORK"
  if [[ -n "${FC_TIMEOUT:-}" ]]; then
    set +e
    timeout --foreground -s KILL "${FC_TIMEOUT}" firecracker --no-api --config-file vm.json
    rc=$?
    set -e
    [[ $rc -eq 124 || $rc -eq 137 ]] && exit 0
    exit $rc
  fi
  exec firecracker --no-api --config-file vm.json
fi

case "@@BOOT_KIND@@" in
  initrd)
    ln -sf "$BOOT" "$WORK/initrd.cpio.gz"
    if [[ -n "$EXTRA_DRIVE_JSON" ]]; then
      DRIVES_ARR="[$EXTRA_DRIVE_JSON]"
    else
      DRIVES_ARR="[]"
    fi
    cat > "$WORK/vm.json" <<JSON
{
  "boot-source": {
    "kernel_image_path": "vmlinux.bin",
    "initrd_path": "initrd.cpio.gz",
    "boot_args": "@@KERNEL_CMDLINE@@ ${CMDLINE_EXTRA}"
  },
  ${NET_FIELD}
  "drives": $DRIVES_ARR,
  "machine-config": {
    "vcpu_count": @@VCPUS@@,
    "mem_size_mib": @@MEM_MIB@@
  }
}
JSON
    ;;
  rootfs)
    # Firecracker opens non-readonly block devices RW; bazel-bin outputs
    # are 0444 so we copy (not symlink) into $WORK and make it writable.
    cp "$BOOT" "$WORK/rootfs.ext4"
    chmod 0644 "$WORK/rootfs.ext4"
    cat > "$WORK/vm.json" <<JSON
{
  "boot-source": {
    "kernel_image_path": "vmlinux.bin",
    "boot_args": "@@KERNEL_CMDLINE@@ root=/dev/vda ${CMDLINE_EXTRA}"
  },
  ${NET_FIELD}
  "drives": [
    {
      "drive_id": "rootfs",
      "path_on_host": "rootfs.ext4",
      "is_root_device": true,
      "is_read_only": false
    }${PERSIST_DRIVE_JSON}
  ],
  "machine-config": {
    "vcpu_count": @@VCPUS@@,
    "mem_size_mib": @@MEM_MIB@@
  }
}
JSON
    ;;
  *)
    echo "runner: unknown boot kind: @@BOOT_KIND@@" >&2
    exit 2
    ;;
esac

cd "$WORK"

if [[ -n "${FC_TIMEOUT:-}" ]]; then
  set +e
  timeout --foreground -s KILL "${FC_TIMEOUT}" firecracker --no-api --config-file vm.json
  rc=$?
  set -e
  # 124 = timeout reached, 137 = killed (128+9). Both are intentional kills.
  if [[ $rc -eq 124 || $rc -eq 137 ]]; then
    exit 0
  fi
  exit $rc
fi

exec firecracker --no-api --config-file vm.json
