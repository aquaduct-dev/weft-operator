<!-- docdaemon ts:2026-10-29T20:38:32Z hash:ca3ec04193d67c80 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory provides a shell wrapper to execute the `gopackagesdriver` tool via Bazel.

## Overview
The `tools/gopls` directory contains infrastructure for integrating Go package discovery with Bazel builds. It enables external tools, such as the Go Language Server (`gopls`), to query package information by delegating execution to the underlying `gopackagesdriver` implementation. This setup ensures that editors and IDEs can leverage Bazel's dependency management and execution context without needing direct access to the build graph.

## Deep Dive
### Wrapper Execution
The primary component is `gopackagesdriver.sh`, which acts as a thin shell interface. It forwards all command-line arguments directly to the `@rules_go//go/tools/gopackagesdriver` Bazel target. This design allows standard invocation patterns used by language servers to work seamlessly within the Bazel sandboxed environment.

### Dependency Management
By routing through Bazel, the driver inherits the correct toolchain and dependency resolution provided by `rules_go`. This avoids manual configuration of GOPATH or module paths, ensuring consistent behavior across different development environments and CI systems.

## Key Interfaces
- `gopackagesdriver.sh`: Entry point script accepting CLI arguments for package querying.

## Dependencies
- `@rules_go//go/tools/gopackagesdriver`: The core Go binary implementing the packages driver protocol.

## Subcomponents
- `gopackagesdriver.sh`: Shell wrapper script.