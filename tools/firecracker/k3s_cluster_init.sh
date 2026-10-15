#!/bin/sh
# /sbin/init — PID 1 for a real multi-node k3s firecracker microVM
# (CRITERIA "No stub tests"). Unlike the single-node k3s_init.sh (which uses
# a dummy interface and only talks to 127.0.0.1), this boots on the REAL
# eth0 the cluster orchestrator wired through a shared bridge, so a server
# VM and agent VMs form a genuine cluster over virtio-net.
#
# Role + join parameters come from the kernel cmdline (set by cluster_run.sh):
#   ofd.role=server|agent
#   ofd.node=<node-name>
#   ofd.token=<shared cluster token>
#   ofd.server=<server IP>            (agents only)
#   ofd.expect=<N>                    (server: nodes to wait for Ready)
#
# Console markers the orchestrator greps:
#   K3SC_BOOTING / K3SC_ROLE:<role>:<node>:<ip>
#   K3SC_AGENT_UP            — agent started k3s agent
#   K3SC_CLUSTER_READY nodes=<n>   — server saw <expect> nodes Ready
#   K3SC_CLUSTER_FAILED            — server timed out
set -u
PATH=/usr/local/bin:/bin:/sbin:/usr/bin:/usr/sbin
export PATH
log() { printf '[k3sc] %s\n' "$*" > /dev/console; }
marker() { printf '%s\n' "$*" > /dev/console; }

mkdir -p /proc /sys /dev /tmp /run /var/run /var/log
mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sysfs /sys 2>/dev/null || true
mount -t devtmpfs devtmpfs /dev 2>/dev/null || true
mount -t tmpfs tmpfs /tmp 2>/dev/null || true
mount -t tmpfs tmpfs /run 2>/dev/null || true
mkdir -p /sys/fs/cgroup
mountpoint -q /sys/fs/cgroup 2>/dev/null || mount -t cgroup2 cgroup2 /sys/fs/cgroup 2>/dev/null || true

CMDLINE="$(cat /proc/cmdline)"
field() { echo "$CMDLINE" | sed -n "s/.*$1=\([^ ]*\).*/\1/p"; }
ROLE="$(field ofd.role)"; [ -n "$ROLE" ] || ROLE=server
NODE="$(field ofd.node)"; [ -n "$NODE" ] || NODE="node0"
TOKEN="$(field ofd.token)"; [ -n "$TOKEN" ] || TOKEN=ofdclustertoken
SERVER="$(field ofd.server)"
EXPECT="$(field ofd.expect)"; [ -n "$EXPECT" ] || EXPECT=1

hostname "$NODE" 2>/dev/null || true
ip link set lo up 2>/dev/null || true
# eth0 is addressed by the kernel ip= autoconfig; capture our IP + ensure a
# default route exists (k3s' kubelet needs one).
SELF="$(ip -4 addr show eth0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p' | head -1)"
GW="$(echo "$CMDLINE" | sed -n 's/.*ip=[0-9.]*::\([0-9.]*\):.*/\1/p')"
[ -n "$GW" ] && ip route show default 2>/dev/null | grep -q default || ip route add default via "$GW" dev eth0 2>/dev/null || true

marker "K3SC_BOOTING"
marker "K3SC_ROLE:${ROLE}:${NODE}:${SELF}"

# Flags common to server + agent. --flannel-backend is SERVER-ONLY (k3s
# agent fatals on it); only --flannel-iface is shared.
COMMON="--flannel-iface=eth0 \
  --kube-proxy-arg=proxy-mode=iptables --node-name=${NODE} --node-ip=${SELF}"

if [ "$ROLE" = "server" ]; then
  EXEC="/usr/local/bin/k3s server --token ${TOKEN} \
    --flannel-backend=host-gw \
    --disable=traefik,servicelb,metrics-server --disable-helm-controller \
    --disable-network-policy --write-kubeconfig-mode=644 \
    --tls-san=${SELF} --tls-san=127.0.0.1 --advertise-address=${SELF} \
    --cluster-cidr=10.244.0.0/16 --service-cidr=10.43.0.0/16 \
    --cluster-dns=10.43.0.10 --data-dir=/var/lib/rancher/k3s ${COMMON}"
else
  EXEC="/usr/local/bin/k3s agent --token ${TOKEN} \
    --server https://${SERVER}:6443 --data-dir=/var/lib/rancher/k3s ${COMMON}"
fi

log "starting ($ROLE): $EXEC"
( $EXEC > /var/log/k3s.log 2>&1 ) &
K3S_PID=$!

if [ "$ROLE" != "server" ]; then
  # Agents just need to join and stay up so the server can see them Ready;
  # the orchestrator's FC_TIMEOUT kills the VM when the run is done.
  sleep 12
  marker "K3SC_AGENT_UP"
  log "agent k3s.log (first 40 lines):"
  head -n 40 /var/log/k3s.log > /dev/console 2>&1 || true
  n=0
  while kill -0 "$K3S_PID" 2>/dev/null && [ "$n" -lt 30 ]; do sleep 5; n=$((n+1)); done
  log "agent k3s.log tail:"
  tail -n 30 /var/log/k3s.log > /dev/console 2>&1 || true
  marker "K3SC_AGENT_EXITED"
  /sbin/poweroff -f; sleep 5; exit 0
fi

# Server: wait until EXPECT nodes are Ready.
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
deadline=$(( $(awk '{print int($1)}' /proc/uptime) + 220 ))
ready=0
while :; do
  now=$(awk '{print int($1)}' /proc/uptime)
  [ "$now" -ge "$deadline" ] && break
  kill -0 "$K3S_PID" 2>/dev/null || { log "k3s server exited early"; break; }
  if [ -f "$KUBECONFIG" ]; then
    n=$(/usr/local/bin/k3s kubectl get nodes --no-headers 2>/dev/null | grep -cE '[[:space:]]Ready[[:space:]]')
    [ -n "$n" ] || n=0
    log "ready nodes=$n / expect=$EXPECT"
    if [ "$n" -ge "$EXPECT" ]; then ready=1; break; fi
  fi
  sleep 3
done

if [ "$ready" = 1 ]; then
  marker "K3SC_CLUSTER_READY nodes=${EXPECT}"
  /usr/local/bin/k3s kubectl get nodes --no-headers > /dev/console 2>&1 || true
else
  marker "K3SC_CLUSTER_FAILED"
  /usr/local/bin/k3s kubectl get nodes > /dev/console 2>&1 || true
  tail -n 40 /var/log/k3s.log > /dev/console 2>&1 || true
fi
sleep 1
/sbin/poweroff -f
sleep 5
exit 0
