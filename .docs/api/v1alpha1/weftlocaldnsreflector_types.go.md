<!-- docdaemon ts:2026-10-29T20:35:46Z hash:5c02d7d8f1703c71 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes Custom Resource Definition (CRD) types for `WeftLocalDNSReflector`, which keeps public Weft traffic inside the cluster.

The `WeftLocalDNSReflector` is a cluster-scoped resource that configures an in-cluster proxy to terminate TLS for specified hostnames, preventing hairpinning through the bastion. It manages three key subsystems:
*   **Routing**: Routes are derived from `WeftTunnel` resources matching the configured `hostnames` or all `allHostnames`.
*   **CoreDNS**: The `ClusterDNSSpec` controls a ConfigMap override in CoreDNS to point pods at the local proxy Service.
*   **Node Trust**: The `NodeTrustSpec` deploys a DaemonSet to inject the CA into node-level containerd and `/etc/hosts`, enabling image pulls to use the local path.

Key types include:
*   `WeftLocalDNSReflectorSpec`: Defines the desired state, including hostname selection (`hostnames` vs `allHostnames`), namespace, replicas, and DNS/node trust configuration. Validation ensures exactly one hostname mode is set and the name length is capped at 40 characters.
*   `WeftLocalDNSReflectorStatus`: Reports observed state, including `ServiceIP`, active `Routes`, unrouted/conflicting hostnames, untrusted pod counts, and readiness conditions.
*   `ReflectedRoute`: Represents a single src→dst pair served by the proxy.

The operator uses this CRD to reconcile the proxy Deployment, Service, Secrets, and associated agents, ensuring traffic stays internal where possible.