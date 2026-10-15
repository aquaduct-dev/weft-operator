#!/usr/bin/env bash
# mkcontainerfs.sh — turn an OCI image layout directory (the output of
# `oci_image`) into a bootable ext4 rootfs by flattening the layers,
# materializing a tiny PID-1 init that mounts /proc /sys /dev, exports
# the image's Env, cd's to WorkingDir, and runs Entrypoint+Cmd.
#
# Whiteouts in AUFS form (.wh.<name> and .wh..wh..opq) are honored when
# applying each layer.
#
# The image is NOT required to ship a shell — we copy a statically-linked
# busybox to /sbin/busybox and the generated /sbin/init uses absolute
# paths. Existing /sbin/init in the image is overwritten by design; this
# rule's whole purpose is to make the container *be* PID 1.
#
# Args:
#   --out PATH               Output ext4 image.
#   --image-dir PATH         OCI image layout directory (index.json + blobs/).
#   --busybox PATH           Statically-linked busybox; copied to /sbin/busybox.
#   --size-mb N              Image size in megabytes.
#   --label STR              Filesystem label (max 16 chars).
# Repeatable:
#   --extra-env KEY=VAL      Env var appended to the image's Env (overrides
#                            same-named keys from the image config).
#   --extra-arg STR          Argument appended after Entrypoint+Cmd.

set -euo pipefail

# bazel's sandbox starts with a minimal PATH; mkfs.ext4 lives in /usr/sbin.
export PATH="/usr/sbin:/sbin:${PATH:-/usr/bin:/bin}"

OUT=""
IMAGE_DIR=""
BUSYBOX=""
SIZE_MB=""
LABEL=""
EXTRA_ENV=()
EXTRA_ARG=()

while [ $# -gt 0 ]; do
  case "$1" in
    --out)        OUT="$2";        shift 2 ;;
    --image-dir)  IMAGE_DIR="$2";  shift 2 ;;
    --busybox)    BUSYBOX="$2";    shift 2 ;;
    --size-mb)    SIZE_MB="$2";    shift 2 ;;
    --label)      LABEL="$2";      shift 2 ;;
    --extra-env)  EXTRA_ENV+=("$2"); shift 2 ;;
    --extra-arg)  EXTRA_ARG+=("$2"); shift 2 ;;
    *) echo "mkcontainerfs: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[ -n "$OUT" ]       || { echo "mkcontainerfs: --out required" >&2; exit 2; }
[ -n "$IMAGE_DIR" ] || { echo "mkcontainerfs: --image-dir required" >&2; exit 2; }
[ -n "$BUSYBOX" ]   || { echo "mkcontainerfs: --busybox required" >&2; exit 2; }
[ -n "$SIZE_MB" ]   || { echo "mkcontainerfs: --size-mb required" >&2; exit 2; }
[ -n "$LABEL" ]     || { echo "mkcontainerfs: --label required" >&2; exit 2; }

command -v mkfs.ext4 >/dev/null || { echo "mkcontainerfs: mkfs.ext4 not found (apt install e2fsprogs)" >&2; exit 1; }
command -v python3   >/dev/null || { echo "mkcontainerfs: python3 required for JSON parsing" >&2; exit 1; }

if ! file -b "$BUSYBOX" | grep -q 'statically linked'; then
  echo "mkcontainerfs: busybox is not statically linked: $BUSYBOX" >&2
  exit 1
fi

WORK="$(mktemp -d -t mkcontainerfs.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/rootfs"
mkdir -p "$ROOT"

# Resolve manifest → config + layer digests via python. jq isn't on the
# default PATH and adding a hermetic jq for one query isn't worth it.
LAYER_LIST="$WORK/layers.txt"
CONFIG_DIGEST="$(python3 - "$IMAGE_DIR" "$LAYER_LIST" <<'PY'
import json, os, sys
image_dir, layer_list = sys.argv[1], sys.argv[2]
with open(os.path.join(image_dir, "index.json")) as f:
    index = json.load(f)
manifest_digest = index["manifests"][0]["digest"].split(":", 1)[1]
with open(os.path.join(image_dir, "blobs", "sha256", manifest_digest)) as f:
    manifest = json.load(f)
print(manifest["config"]["digest"].split(":", 1)[1])
with open(layer_list, "w") as out:
    for l in manifest["layers"]:
        out.write(l["digest"].split(":", 1)[1] + "\n")
PY
)"

CONFIG_BLOB="$IMAGE_DIR/blobs/sha256/$CONFIG_DIGEST"

# Apply each layer, honoring AUFS-style whiteouts. Each layer is extracted
# to a per-layer staging dir first so we can scan for `.wh.*` markers
# before merging onto $ROOT.
while IFS= read -r digest; do
  [ -z "$digest" ] && continue
  blob="$IMAGE_DIR/blobs/sha256/$digest"
  [ -f "$blob" ] || { echo "mkcontainerfs: missing layer blob $digest" >&2; exit 1; }
  layer_dir="$WORK/layer-$digest"
  mkdir -p "$layer_dir"
  case "$(file -b "$blob")" in
    *gzip*) tar -xzf "$blob" -C "$layer_dir" ;;
    *zstd*) tar --use-compress-program=zstd -xf "$blob" -C "$layer_dir" ;;
    *)      tar -xf  "$blob" -C "$layer_dir" ;;
  esac

  while IFS= read -r -d '' wh; do
    name="${wh##*/}"
    dir_in_tmp="${wh%/*}"
    rel_dir="${dir_in_tmp#$layer_dir}"
    target_dir="$ROOT$rel_dir"
    if [ "$name" = ".wh..wh..opq" ]; then
      if [ -d "$target_dir" ]; then
        find "$target_dir" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
      fi
    else
      base="${name#.wh.}"
      rm -rf -- "$target_dir/$base"
    fi
    rm -f -- "$wh"
  done < <(find "$layer_dir" -name '.wh.*' -print0)

  if [ -n "$(ls -A "$layer_dir" 2>/dev/null)" ]; then
    cp -aT "$layer_dir" "$ROOT"
  fi
  rm -rf "$layer_dir"
done < "$LAYER_LIST"

# Standard kernel mount points (some images have these, some don't).
mkdir -p "$ROOT/proc" "$ROOT/sys" "$ROOT/dev" "$ROOT/tmp" "$ROOT/run" "$ROOT/sbin"
chmod 1777 "$ROOT/tmp"

# Some images ship /sbin/init as a symlink to busybox (alpine) or as a
# read-only regular file. Unlink before writing so we can install our own.
rm -f "$ROOT/sbin/init" "$ROOT/sbin/busybox"
install -m 0755 "$BUSYBOX" "$ROOT/sbin/busybox"

# Generate /sbin/init from the image config + extra env/args. Python does
# the JSON read and the shell-quoting in one pass so we avoid juggling
# NUL-delimited lists through shell variables (which strip NUL bytes).
EXTRA_ENV_FILE="$WORK/extra_env.txt"
EXTRA_ARG_FILE="$WORK/extra_arg.txt"
: >"$EXTRA_ENV_FILE"
: >"$EXTRA_ARG_FILE"
for kv in "${EXTRA_ENV[@]:-}"; do
  [ -z "$kv" ] && continue
  case "$kv" in
    *=*) printf '%s\n' "$kv" >>"$EXTRA_ENV_FILE" ;;
    *) echo "mkcontainerfs: --extra-env entry missing '=': $kv" >&2; exit 2 ;;
  esac
done
for a in "${EXTRA_ARG[@]:-}"; do
  [ -z "$a" ] && continue
  printf '%s\n' "$a" >>"$EXTRA_ARG_FILE"
done

python3 - "$CONFIG_BLOB" "$EXTRA_ENV_FILE" "$EXTRA_ARG_FILE" "$ROOT/sbin/init" <<'PY'
import json, shlex, sys

config_path, extra_env_path, extra_arg_path, init_path = sys.argv[1:5]

with open(config_path) as f:
    cfg = (json.load(f).get("config") or {})

env = list(cfg.get("Env") or [])
entrypoint = list(cfg.get("Entrypoint") or [])
cmd = list(cfg.get("Cmd") or [])
workdir = cfg.get("WorkingDir") or ""

with open(extra_env_path) as f:
    extra_env = [ln.rstrip("\n") for ln in f if ln.strip()]
with open(extra_arg_path) as f:
    extra_args = [ln.rstrip("\n") for ln in f if ln.strip()]

argv = entrypoint + cmd + extra_args
if not argv:
    print("mkcontainerfs: image has no Entrypoint or Cmd and no --extra-arg", file=sys.stderr)
    sys.exit(1)

lines = [
    "#!/sbin/busybox sh",
    "# Generated by //tools/firecracker:mkcontainerfs - do not edit.",
    "set -u",
    "/sbin/busybox mount -t proc proc /proc 2>/dev/null || true",
    "/sbin/busybox mount -t sysfs sysfs /sys 2>/dev/null || true",
    "/sbin/busybox mount -t devtmpfs devtmpfs /dev 2>/dev/null || true",
    "/sbin/busybox mount -t tmpfs tmpfs /tmp 2>/dev/null || true",
    "/sbin/busybox mount -t tmpfs tmpfs /run 2>/dev/null || true",
]

# Image Env first, --extra-env after — later wins on duplicate keys.
for entry in list(env) + list(extra_env):
    if "=" not in entry:
        continue
    k, v = entry.split("=", 1)
    lines.append("export {}={}".format(k, shlex.quote(v)))

if workdir:
    lines.append("cd {} 2>/dev/null || true".format(shlex.quote(workdir)))

lines.append('echo "FIRECRACKER_CONTAINER_READY"')

# Run as a child (not exec) so we can poweroff cleanly when it exits.
# Kernel panics if PID 1 exits, but stays alive as long as init is running.
cmdline = " ".join(shlex.quote(a) for a in argv)
lines.append(cmdline)
lines.append("rc=$?")
lines.append('echo "FIRECRACKER_CONTAINER_EXIT=$rc"')
lines.append("/sbin/busybox poweroff -f")

with open(init_path, "w") as f:
    f.write("\n".join(lines) + "\n")
PY
chmod 0755 "$ROOT/sbin/init"

# Sparse zero-fill, then format with the staged tree as -d seed.
truncate -s "${SIZE_MB}M" "$OUT"
mkfs.ext4 -F -q -L "$LABEL" -d "$ROOT" "$OUT" >/dev/null
