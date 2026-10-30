# Weft Operator

[![Test](https://github.com/aquaduct-dev/weft-operator/actions/workflows/test.yml/badge.svg)](https://github.com/aquaduct-dev/weft-operator/actions/workflows/test.yml)
[![Release](https://github.com/aquaduct-dev/weft-operator/actions/workflows/release.yml/badge.svg)](https://github.com/aquaduct-dev/weft-operator/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/aquaduct-dev/weft-operator?label=release&sort=semver)](https://github.com/aquaduct-dev/weft-operator/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/aquaduct-dev/weft-operator)](go.mod)
[![Container image](https://img.shields.io/badge/image-ghcr.io%2Faquaduct--dev%2Fweft--operator-blue?logo=github)](https://github.com/aquaduct-dev/weft-operator/pkgs/container/weft-operator)
[![Helm chart](https://img.shields.io/badge/chart-ghcr.io%2Faquaduct--dev%2Fcharts%2Fweft--operator-0F1689?logo=helm&logoColor=white)](https://github.com/aquaduct-dev/weft-operator/pkgs/container/charts%2Fweft-operator)

This repo contains code for the Weft operator.  

The `weft` Operator is designed to make exposing a Kubernetes homelab to the internet easy.  
It's written with `controller-runtime` and tested with `envtest`.
It invokes the `weft` CLI by using container `ghcr.io/aquaduct-dev/weft` with specific args.

## Quick Start

### Installation

To install the `weft-operator` using Helm, run the following command:

```bash
helm install weft-operator oci://ghcr.io/aquaduct-dev/charts/weft-operator:latest
```

### Anatomy of a Weft Tunnel

Weft allows requests from the internet to hit a **Weft server**, which serves as a frontend for potentially many services.  The **Weft server** is connected over WireGuard to one **Weft tunnel** per service.

When a user requests a specific resource on the **Weft server**:
1. The **Weft server**  identifies which WireGuard address is serving the request.
2. The request is proxied to the **Weft tunnel** on that WireGuard address.
3. The **Weft tunnel** proxies the request to the ultimate backend.
3. The response is proxied back.

### Required Network Configuration

At least one node in your cluster must be publically accessible from the internet.  On a home network, this is generally accomplished by setting a DMZ host or opening all ports.  If a host is set up correctly, Weft will automatically use it to run a bastion.

### Your first Weft Tunnel

To expose service `service` in namespace `ns` on `https://example.com`, create the following CRD:

```yaml
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftTunnel
metadata:
  name: example-tunnel
  namespace: default
spec:
  srcURL: "http://service.ns.svc.cluster.local:8080"
  dstURL: "https://example.com"
```

If domain `example.com` points to the server IP, after a minute you will be able to view `service` through that domain with HTTPS set up.

## CRDs

Several CRDs are implemented by this operator.

### `WeftServer`

This CRD is used to control a `Deployment` of `weft server`, using the host network of the bastion.  

The deployment is updated to maintain an instance of `weft server` running on the single host node with host network access.

This CRD contains the following information:
 - the server's connection string (`Spec.ConnectionString`)
   - Values for `--bind-ip`, `--connection-secret`, and `--port` will be parsed from this string
   - The string is a URL in the format `weft://<secret>@<bind_ip>:<port>`
 - any `--bind-interface` for the server (`Spec.BindInterface`)
 - any `--usage-reporting-url` for the server (`Spec.UsageReportingURL`)
 - any `--cloudflare-token` for the server (`Spec.CloudflareTokenSecretRef`)
 - the status of the server (obtained by calling the list endpoint, `Status.Tunnels`)
 - whether or not this server is internal to the cluster (`Spec.Location`)

The one exception to this is if the `Spec.Location` is set to `External` in which case no deployment is created (the host is running outside the cluster).  Status `External` bastions are intended to be reconciled by `AquaductTaaS`.

The command for the deployment is `weft server --connection-secret=<connection_secret> --bind-ip=<bind_ip> --bind-interface=<bind_interface> [other flags]`.

#### Scheduled Listener for `weft probe`
This reconciler must also periodically (every 3h) run `weft probe` on each node in `host` networking mode.  This command determines if the node is an internet-routable `WeftServer` candidate.

If a node is internet-routable, the reconciler should automatically create a `WeftServer` on it with a random 10-character connection secret.

#### Example

```yaml
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftServer
metadata:
  name: example-weftserver
  namespace: default
spec:
  # Connection string in format weft://<secret>@<bind_ip>:<port>
  connectionString: "weft://mysecret@192.168.1.100:8080"
  location: Internal
  # Optional: Cloudflare token for DNS updates
  # cloudflareTokenSecretRef:
  #   name: cloudflare-token
  #   key: token
```

### `WeftTunnel`

This CRD is used to represent a tunnel. It is used to control multiple `Deployment`s of `weft tunnel`.  It has the following features:

 - It can specify which `WeftServer`s tunnel deployments should connect to for load balancing
    - If it specifies none, it will be deployed to all `WeftServer`s
    - Connection strings can be read from the `WeftServer` CRD
 - It specifies `Spec.SrcUrl` and `Spec.DstUrl`
 - No tokens are stored in the `WeftTunnel` CRD.  Instead, they are fetched from `WeftServer` resources and are directly injected into the tunnel deployments.

The command for the deployment is `weft tunnel --tunnel-name=<tunnel-name> <weft://server-address> <src_url> <dst_url>`.

#### Example

```yaml
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftTunnel
metadata:
  name: example-tunnel
  namespace: default
spec:
  srcURL: "tcp://0.0.0.0:2222"
  dstURL: "tcp://localhost:22"
  # Optional: Connect only to specific servers
  targetServers:
    - example-weftserver
```
 

### `WeftGateway` implementation of `GatewayClass`

The `weft` operator implements the Kubernetes Gateway API by managing `Gateway` resources. It does this by leveraging the `WeftGateway` CRD as a parameter for `GatewayClass` to configure Weft-specific settings, such as which `WeftServer`s to use.

#### Example: Gateway API `Gateway` resource

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: example-gateway
  namespace: default
spec:
  gatewayClassName: weft-gateway-class # Assumes 'weft-gateway-class' is defined and uses WeftGateway parametersRef
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: example.com
```

### `AquaductTaaS`

This CRD connects the cluster to the user's online `aquaduct.dev` account via a long-lived access token stored in a Secret. The reconciler calls the aquaduct.dev REST API (see https://aquaduct.dev/api/openapi.json) to:

 - Mirror every cloud-hosted bastion into the cluster as a `WeftServer` with `Location: External`
 - Suspend those bastions via `PATCH /api/bastion/{id}` when the `AquaductTaaS` CR is deleted (a finalizer blocks deletion until suspend succeeds)

#### Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aquaduct-token
  namespace: default
stringData:
  token: "your-long-lived-access-token"
---
apiVersion: weft.aquaduct.dev/v1alpha1
kind: AquaductTaaS
metadata:
  name: example-aquaduct-taas
  namespace: default
spec:
  accessTokenSecretRef:
    name: aquaduct-token
    key: token
```


### `WeftLocalDNSReflector`

Traffic from inside the cluster to a public weft hostname normally hairpins: pod → bastion → WireGuard tunnel → back into the cluster. For bulk transfers (container image pushes and pulls against an in-cluster registry, for example) that round trip is the bottleneck. A `WeftLocalDNSReflector` keeps that traffic in the cluster.

It is cluster-scoped. It serves either the hostnames listed in `spec.hostnames` or, with `spec.allHostnames: true`, every http/https hostname any `WeftTunnel` serves. Two reflectors never serve the same hostname. An explicit `hostnames` entry beats `allHostnames`, the oldest claim wins among equals, and the loser lists the hostname in `status.conflictingHostnames`. For each hostname it serves, it:

1. Finds the `WeftTunnel`s whose `dstURL` has that host (including every tunnel a weft `Gateway` generates) and reuses their `srcURL` backends, so routing is defined once.
2. Issues a self-signed CA (Secret `<name>-ca`) and a serving certificate for the hostnames (Secret `<name>-tls`), renewed automatically 30 days before expiry.
3. Runs `weft proxy --tls-cert … --tls-key …` (Deployment and ClusterIP Service `<name>-proxy`, ports 443/80) serving those backends.
4. **Makes clients trust the CA, with no change to any workload:**
   - The operator publishes ConfigMap `weft-trust-bundle` in every namespace: the operator image's system roots plus every reflector CA.
   - A mutating admission webhook (`weft-operator-trust-injector`, managed by the operator, `failurePolicy: Ignore`) injects it into every new pod. It mounts the bundle over `/etc/ssl/certs/ca-certificates.crt`, `/etc/pki/tls/certs/ca-bundle.crt` and `/etc/ssl/cert.pem`. That covers everything that reads the system trust store: Go, OpenSSL and GnuTLS (curl, git), Python's `ssl`. Container environment variables are never touched. Containers that mount their own `/etc/ssl` (or a parent of these paths) keep it.
   - On every node, the node agent appends the CA to the host's CA bundle and to containerd's `certs.d/<hostname>/hosts.toml`.
5. **Trust gate.** The webhook only sees pods as they are created. So while any running pod does not yet trust this CA (it predates the reflector, or was admitted while the webhook was down), steps 6 and 7 are held back and `status.untrustedPods` / `status.untrustedPodSample` name those pods. Restart them, or set `spec.ignoreUntrustedPods: true`. Turning a reflector on therefore never breaks a running pod; traffic keeps using the tunnel until it is safe.
6. Once the proxy has an available replica and the gate is open, writes `<hostname>:53 { hosts { <service IP> <hostname> } }` blocks under key `weft-local-dns-reflector-<name>.server` in `kube-system/coredns-custom` (the k3s Corefile imports `*.server` from it). If either condition stops holding, the key is withdrawn and pods resolve the public address again.
7. Under the same gate, runs the node agent DaemonSet (`<name>-node-agent`, the operator image's `node-agent` subcommand). Kubelet/containerd image pulls and host processes use the node's resolver, not CoreDNS, so the agent adds an `/etc/hosts` entry pointing at the Service IP, **only while a verified TLS handshake to the proxy succeeds**. It removes everything it wrote when stopped.

Pods in `kube-system` and in the operator's namespace are never injected and are exempt from the gate. Opt a pod out with the label or annotation `weft.aquaduct.dev/inject-trust: "false"`. Runtimes that ignore the system store and ship their own CA list are not covered: Node.js, Python `requests` (certifi) and Java.

#### The cluster-default reflector

By default the operator creates and keeps a reflector named `cluster-default` with `allHostnames: true`. Every hostname published through weft is then served in-cluster with no per-cluster setup. Chart values:

```yaml
defaultReflector:
  enabled: true          # false: the operator deletes the reflector it created
  name: cluster-default
  hostnames: []          # empty = allHostnames; or an explicit list
```

The operator only ever modifies or deletes a reflector carrying its `weft.aquaduct.dev/default-reflector: "true"` label, so a hand-made reflector with the same name is left alone. Hand-made reflectors with explicit hostnames take precedence over the default one.

#### Example

```yaml
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftLocalDNSReflector
metadata:
  name: weft-local
spec:
  hostnames:
    - gitea.example.com
  # Defaults shown:
  # namespace: weft-system
  # replicas: 1
  # clusterDNS: {configMapNamespace: kube-system, configMapName: coredns-custom}
  # nodeTrust: {containerdCertsDir: /var/lib/rancher/k3s/agent/etc/containerd/certs.d}
```

`kubectl get wldr` shows the Service IP, untrusted pod count and readiness; `status.routes` lists what is served and `status.unroutedHostnames` lists hostnames with no matching `WeftTunnel`.

Requires a weft image whose `weft proxy` supports `--tls-cert`/`--tls-key` (set `WEFT_IMAGE` on the operator to override `ghcr.io/aquaduct-dev/weft:latest`), and `WEFT_OPERATOR_IMAGE` (set by the chart) for the node agent.

## End-to-end tests

`e2e/` holds hermetic end-to-end tests that boot a single-node k3s inside an
airgapped firecracker microVM, using
[rules_firecracker](https://github.com/aquaduct-dev/rules_firecracker). Every image the VM needs is built or pinned by Bazel and
preloaded, the operator is installed from the rendered chart, and weft is
built from source (the `aquaduct_weft` dev dependency in `MODULE.bazel`).

```bash
bazel test --config=kvm //e2e/...
```

The host needs `/dev/kvm` (user in the `kvm` group; firecracker itself is pinned by the rules)
and gcc/make/flex/bison/libelf-dev for the one-time guest kernel build.
KVM-less hosts can skip these tests with `--test_tag_filters=-requires-kvm`.

To test against an unpushed weft checkout, add to `.bazelrc.user`:

```
common --override_module=aquaduct_weft=/path/to/weft
```

`//e2e/reflector:default_reflector_test` installs the chart with its default values and checks that the operator's `cluster-default` reflector serves a tunnelled hostname in-cluster with no reflector created by hand. It then re-applies the chart with `defaultReflector.enabled=false` and checks that the reflector, its DNS override and its node entries are removed.

`//e2e/reflector:reflector_test` proves a `WeftLocalDNSReflector` keeps
traffic in the cluster. It publishes a registry as `registry.weft-e2e.test`,
a name that cannot resolve except through the reflector, and checks that:

- the trust gate holds DNS back while an older pod lacks the CA;
- a pod resolves the name to the proxy and verifies its certificate with only the system trust store;
- that pod pushes an image through the proxy;
- containerd pulls the image back through the node agent's entries;
- a plain process on the k3s node (outside any pod) resolves the name through the node's `/etc/hosts` and accepts the proxy certificate from the node's system trust store. A control run against the node's original bundle must be rejected as an unknown authority;
- the registry's access log shows every request arriving from the proxy;
- deleting the reflector removes everything it wrote, restoring the node trust store byte for byte.
