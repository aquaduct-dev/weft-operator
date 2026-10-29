<!-- docdaemon ts:2026-10-29T20:36:48Z hash:4c30167507196b7c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script initializes a multi-node k3s cluster within Firecracker microVMs, coordinating server and agent roles via kernel command-line parameters.

It parses `ofd.role`, `ofd.node`, `ofd.token`, and network configuration from `/proc/cmdline`. It mounts essential filesystems (`proc`, `sysfs`, `devtmpfs`) and configures the network interface `eth0` using auto-assigned IPs and gateways.

For **server** nodes, it launches `k3s server` with specific CIDR ranges and disabled components (traefik, servicelb). It then polls `kubectl get nodes` until the expected number of agents reach `Ready` status or a timeout occurs, emitting `K3SC_CLUSTER_READY` or `K3SC_CLUSTER_FAILED` markers to the console.

For **agent** nodes, it starts `k3s agent` pointing to the server IP. After a brief stabilization period, it logs initial startup details and exits, allowing the orchestrator to power off the VM.

The script ensures genuine cluster formation over `virtio-net` rather than loopback-only testing, supporting e2e scenarios like in-cluster DNS resolution through reflectors.