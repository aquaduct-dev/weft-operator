<!-- docdaemon ts:2026-10-29T20:39:36Z hash:942da9a1d6722829 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the Bazel build configuration and the Go entry point for compiling, packaging, and running the Weft Operator.

## Overview
The `cmd` directory serves as the bridge between the operator's internal logic and its deployment lifecycle. It defines how the binary is cross-compiled into multi-architecture OCI images and provides the main executable that bootstraps the Kubernetes controller-runtime manager, registers controllers, and handles node agent execution.

## Deep Dive
### Build System
`BUILD.bazel` orchestrates the creation of the `weft_operator_image`. It compiles a private `cmd_lib` containing the core logic, then uses `go_cross_binary` to produce binaries for `linux_amd64` and `linux_arm64`. These are layered into Alpine-based OCI images and merged via an index. A public push rule publishes to `ghcr.io/aquaduct-dev/weft-operator`, while a load rule supports local development by injecting the AMD64 variant into the Docker daemon.

### Application Bootstrap
`main.go` initializes the controller-runtime manager with a filtered cache restricted to ConfigMaps labeled `trust.ManagedLabel` to prevent OOM errors. It performs a preflight check to ensure Gateway API CRDs exist before starting. The file registers six reconcilers: `WeftServer`, `WeftTunnel`, `AquaductTaaS`, `DNSRecord`, `WeftGateway`, and `WeftLocalDNSReflector`. It also configures a trust injection webhook for CA certificates and supports a `node-agent` subcommand for direct node execution.

## Key Interfaces
- `main()` Entry point for the operator binary and node agent.
- `cmd_lib` Private Go library exposing the operator's initialization logic.

## Dependencies
- Kubernetes client-go libraries
- Controller-runtime
- Internal controllers (`weftgateway`, `dnsrecord`)
- Alpine Linux base image

## Subcomponents
- `BUILD.bazel`: Bazel build rules for compilation and image packaging.
- `main.go`: Application entry point and manager bootstrap.