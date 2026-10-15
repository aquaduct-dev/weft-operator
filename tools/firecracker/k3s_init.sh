#!/bin/sh
# /sbin/init — PID 1 inside the firecracker_k3s_test microVM.
#
# Boots a single-node k3s server, optionally imports OCI image tarballs
# into the embedded containerd, applies any baked-in YAML manifests,
# then exec's the guest-side test binary and powers off when it exits.
#
# Markers printed to /dev/console (scraped by gen_k3s_test.sh on the host):
#
#   K3S_BOOTING          — start of boot, after mounts
#   K3S_READY            — k3s server reports node Ready
#   K3S_FAILED           — k3s never came up within the deadline
#   SCENARIO_BEGIN:<name>
#   ===REPORT_BEGIN===
#   <multiline report JSON>
#   ===REPORT_END===
#   SCENARIO_END:<name>:<exit_code>
#   K3S_POWEROFF
#
# Everything between REPORT_BEGIN/REPORT_END is the canonical report
# JSON that satisfies the lib/clusterharness.Report shape — the host
# launcher copies those bytes verbatim to $SCENARIO_REPORT_PATH.

set -u
PATH=/usr/local/bin:/bin:/sbin:/usr/bin:/usr/sbin
export PATH

log() { printf '[k3s-init] %s\n' "$*" > /dev/console; }
marker() { printf '%s\n' "$*" > /dev/console; }

# --- Stage 1: mount kernel filesystems ---
mkdir -p /proc /sys /dev /tmp /run /var/run /var/log
mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sysfs /sys 2>/dev/null || true
mount -t devtmpfs devtmpfs /dev 2>/dev/null || true
mount -t tmpfs tmpfs /tmp 2>/dev/null || true
mount -t tmpfs tmpfs /run 2>/dev/null || true

# cgroup v2 unified hierarchy. k3s requires either v1 or v2; v2 is the
# default since the 5.x kernels firecracker now ships, and is what
# k3s-1.30 expects.
mkdir -p /sys/fs/cgroup
if ! mountpoint -q /sys/fs/cgroup 2>/dev/null; then
  mount -t cgroup2 cgroup2 /sys/fs/cgroup 2>/dev/null || \
    log "warning: cgroup2 mount failed; k3s may not start"
fi

# Hostname must be a valid k8s label value (alphanumeric + '-_.'); kubelet
# uses it for the node name and for several metadata.labels entries.
# Without an explicit hostname busybox returns "(none)" which fails
# apiserver lease validation: `Invalid value: "(none)"`.
hostname server0 2>/dev/null || true

# Bring loopback up — k3s binds the apiserver on 0.0.0.0:6443 but tests
# (and k3s' own internal clients) hit 127.0.0.1.
ip link set lo up 2>/dev/null || ifconfig lo 127.0.0.1 up 2>/dev/null || true

# k3s' embedded kubelet calls `net.ChooseHostInterface()` which reads
# /proc/net/route looking for a default route; without one k3s fatals
# with "no default routes found". The firecracker microVM has no
# virtio-net device (we don't grant CAP_NET_ADMIN to the bazel sandbox
# for tap setup), so we synthesize a dummy interface — CONFIG_DUMMY=y
# is enabled in vmlinux-6.1.config specifically for this. The 10.42.0.10
# address never carries traffic; kubelet/apiserver are still reached at
# 127.0.0.1:6443 from the guest test binary, but the apiserver's
# kubernetes Service endpoint requires a non-loopback advertise-address,
# and kubelet rejects 127.0.0.1 as nodeIP — both satisfied by 10.42.0.10.
ip link add dev fakenet type dummy 2>/dev/null || \
  log "warning: ip link add type dummy failed (CONFIG_DUMMY=y required)"
ip addr add 10.42.0.10/24 dev fakenet 2>/dev/null || true
ip link set dev fakenet up 2>/dev/null || true
ip route add default via 10.42.0.1 dev fakenet 2>/dev/null || true

log "network state after setup:"
ip addr 2>/dev/null > /dev/console || true
ip route 2>/dev/null > /dev/console || true

# Random entropy. Without random.trust_cpu the apiserver's TLS cert
# generation can stall for ~60s. Pass it as a kernel cmdline arg (set
# by tools/firecracker:defs.bzl's _DEFAULT_CMDLINE).
[ -e /dev/random ] && [ -e /dev/urandom ] || log "warning: no /dev/random"

marker "K3S_BOOTING"

# --- Stage 2: preload images into k3s' embedded containerd ---
# k3s' image store lives under /var/lib/rancher/k3s/agent/images/. Files
# dropped there at startup are picked up by the agent and imported into
# containerd. We don't strictly need that path though — once the server
# is up we can use `k3s ctr images import` directly. The simpler path:
# copy any /etc/images/*.tar files into the auto-import directory.
mkdir -p /var/lib/rancher/k3s/agent/images
for img in /etc/images/*.tar /etc/images/*.tar.gz /etc/images/*.tgz; do
  [ -f "$img" ] || continue
  cp "$img" /var/lib/rancher/k3s/agent/images/
  log "preloaded image $(basename "$img")"
done

# --- Stage 3: start k3s server in the background ---
# Flags chosen to keep the test deterministic + cheap:
#   --disable=traefik,servicelb,metrics-server: skip the addons we don't
#     test; metrics-server in particular wants to pull from gcr.io.
#   --disable-helm-controller: we install charts from the host side, no
#     in-cluster helm needed.
#   --disable-network-policy: skip the kube-router CNI ext.
#   --flannel-backend=host-gw: avoids the vxlan kernel module the
#     pinned 6.1.102 firecracker vmlinux may not ship.
#   --kube-proxy-arg=proxy-mode=iptables: iptables is in the binary.
#   --write-kubeconfig-mode=644: so the guest test binary can read it
#     without being root (it IS root, but explicit is better).
#   --tls-san=127.0.0.1: the apiserver cert needs to cover the loopback
#     address our test binary hits.
mkdir -p /etc/rancher/k3s

# Node IP = fake interface IP (10.42.0.10). Apiserver still binds
# 0.0.0.0:6443 by default so 127.0.0.1:6443 from inside the VM works
# for the guest test binary; we just can't bind-address it to 127.0.0.1
# because then the kubernetes Service endpoint becomes invalid.
K3S_EXEC="/usr/local/bin/k3s server \
  --disable=traefik,servicelb,metrics-server \
  --disable-helm-controller \
  --disable-network-policy \
  --flannel-backend=host-gw \
  --flannel-iface=fakenet \
  --kube-proxy-arg=proxy-mode=iptables \
  --write-kubeconfig-mode=644 \
  --tls-san=127.0.0.1 \
  --tls-san=10.42.0.10 \
  --tls-san=localhost \
  --node-name=server0 \
  --node-ip=10.42.0.10 \
  --advertise-address=10.42.0.10 \
  --cluster-cidr=10.244.0.0/16 \
  --service-cidr=10.43.0.0/16 \
  --cluster-dns=10.43.0.10 \
  --data-dir=/var/lib/rancher/k3s"

log "starting k3s: $K3S_EXEC"
# Send k3s logs ONLY to /var/log/k3s.log — never to /dev/console.
# k3s logs pour out continuously and would interleave with the marker
# block / report JSON the host launcher scrapes between
# ===REPORT_BEGIN===/===REPORT_END===, breaking jq parsing.
# (When k3s fails to come up, the failure path tails the log to console.)
( $K3S_EXEC > /var/log/k3s.log 2>&1 ) &
K3S_PID=$!

# --- Stage 4: wait for k3s to be Ready ---
# Two signals: kubeconfig file appears, then `kubectl get nodes` shows Ready.
deadline=$(( $(awk '{print int($1)}' /proc/uptime 2>/dev/null || echo 0) + 240 ))
kubeconfig_ok=0
node_ready=0
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
while :; do
  now=$(awk '{print int($1)}' /proc/uptime 2>/dev/null || echo 0)
  if [ "$now" -ge "$deadline" ]; then
    break
  fi
  if ! kill -0 "$K3S_PID" 2>/dev/null; then
    log "k3s exited prematurely"
    break
  fi
  if [ "$kubeconfig_ok" -eq 0 ] && [ -f "$KUBECONFIG" ]; then
    kubeconfig_ok=1
    log "kubeconfig present"
  fi
  if [ "$kubeconfig_ok" -eq 1 ]; then
    if /usr/local/bin/k3s kubectl get nodes 2>/dev/null | grep -qE '^server0[[:space:]]+(Ready|Ready,)'; then
      node_ready=1
      break
    fi
  fi
  sleep 2
done

if [ "$node_ready" -ne 1 ]; then
  marker "K3S_FAILED"
  log "k3s did not report node Ready within deadline; tail of k3s.log:"
  tail -n 60 /var/log/k3s.log > /dev/console 2>&1 || true
  marker "===REPORT_BEGIN==="
  scen=$(cat /etc/scenario 2>/dev/null || echo unknown)
  cat > /tmp/report.json <<EOF
{
  "scenario": "$scen",
  "pass_conditions": [
    { "id": "boot.k3s-ready", "label": "k3s-ready", "passed": false, "error": "k3s did not reach Ready inside the firecracker microVM" }
  ]
}
EOF
  cat /tmp/report.json > /dev/console
  marker ""
  marker "===REPORT_END==="
  marker "SCENARIO_END:$(cat /etc/scenario 2>/dev/null || echo unknown):1"
  marker "K3S_POWEROFF"
  /sbin/poweroff -f
  sleep 5
  exit 1
fi

marker "K3S_READY"

# --- Stage 5: apply baked manifests ---
for m in /etc/manifests/*.yaml /etc/manifests/*.yml; do
  [ -f "$m" ] || continue
  log "applying manifest $(basename "$m")"
  if ! /usr/local/bin/k3s kubectl apply -f "$m" > /dev/console 2>&1; then
    log "manifest $(basename "$m") apply failed (continuing)"
  fi
done

# --- Stage 6: run the test binary ---
scen=$(cat /etc/scenario 2>/dev/null || echo unknown)
tbin=$(cat /etc/test_binary 2>/dev/null || echo)
[ -n "$tbin" ] || tbin=thirdeyetest

marker "SCENARIO_BEGIN:$scen"
mkdir -p /tmp
/usr/local/bin/"$tbin" \
  --scenario="/etc/manifests/$scen.yaml" \
  --report-json=/tmp/report.json \
  --kubeconfig="$KUBECONFIG" \
  --namespace=default \
  > /tmp/test_stdout.log 2>&1
tcode=$?
log "test binary exit=$tcode"
# Tee stdout/stderr to /dev/console so failures are inspectable.
cat /tmp/test_stdout.log > /dev/console 2>&1 || true

marker "===REPORT_BEGIN==="
if [ -s /tmp/report.json ]; then
  cat /tmp/report.json > /dev/console
else
  cat > /tmp/report.json <<EOF
{
  "scenario": "$scen",
  "pass_conditions": [
    { "id": "harness.exit", "label": "test-binary-exit", "passed": false, "error": "test binary produced no report (exit=$tcode)" }
  ]
}
EOF
  cat /tmp/report.json > /dev/console
fi
marker ""
marker "===REPORT_END==="
marker "SCENARIO_END:$scen:$tcode"
marker "K3S_POWEROFF"

# Give the serial console a beat to flush before the kernel shuts down.
sleep 1
/sbin/poweroff -f
sleep 5
exit "$tcode"
