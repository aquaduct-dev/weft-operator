# Weft Operator

[![Test](https://github.com/aquaduct-dev/weft-operator/actions/workflows/test.yml/badge.svg)](https://github.com/aquaduct-dev/weft-operator/actions/workflows/test.yml) [![Release](https://github.com/aquaduct-dev/weft-operator/actions/workflows/release.yml/badge.svg)](https://github.com/aquaduct-dev/weft-operator/actions/workflows/release.yml)

This repo contains code for the Weft operator.  

The `weft` Operator is designed to make exposing a Kubernetes homelab to the internet easy.  
It's written with `controller-runtime` and tested with `envtest`.
It invokes the `weft` CLI by using container `ghcr.io/aquaduct-dev/weft` with specific args.

## Quick Start

To install the `weft-operator` using Helm, run the following command:

```bash
helm install weft-operator oci://ghcr.io/aquaduct-dev/charts/weft-operator:latest
```

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

If a node is internet-routable, the reconciler should automatically create a `WeftServer` CR on it, with a random 10-character connection secret and `--bind-ip` set to the IP

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

This CRD stores information about the user's online `aquaduct.dev` account.  Specifically, it stores the user's long-lived access token.  Then, by connecting to `api.aquaduct.dev`, it is intended to reconcile the following information into the cluster (not yet implemented):

 - Any external (i.e. hosted in the cloud) `WeftServer`s and their tokens

#### Example

```yaml
apiVersion: weft.aquaduct.dev/v1alpha1
kind: AquaductTaaS
metadata:
  name: example-aquaduct-taas
  namespace: default
spec:
  accessToken: "your-long-lived-access-token"
```

