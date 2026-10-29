<!-- docdaemon ts:2026-10-29T20:35:57Z hash:0bf10feb0246df55 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file derives in-cluster proxy routes from WeftTunnel resources to optimize internal traffic.

`computeRoutes` matches requested hostnames against tunnel destinations, ensuring stable selection by sorting tunnels and deduplicating conflicts. It returns a `routeSet` containing valid routes, unrouted hostnames, and skipped reasons.

`reflectTunnel` transforms a tunnel’s source/destination into a proxy route, validating schemes (http/https only) and rejecting loopback sources. It adjusts the destination port to the proxy container’s internal ports (`8443` for HTTPS, `8080` for HTTP).

`proxyArgs` generates the command-line arguments for the `weft proxy` binary, including TLS certificate paths and the derived src→dst pairs.

The design prioritizes deterministic output for unit testing and avoids redundant routing that would cause the proxy to fail on startup.