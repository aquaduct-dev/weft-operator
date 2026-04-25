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

