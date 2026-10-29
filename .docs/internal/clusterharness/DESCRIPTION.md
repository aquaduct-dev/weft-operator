<!-- docdaemon ts:2026-10-29T20:38:48Z hash:060a10e1ea766e1f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `clusterharness` package provides the infrastructure and JSON reporting contract for running end-to-end tests in isolated firecracker microVMs or against live Kubernetes clusters.

## Overview
This directory bridges the gap between guest-side test binaries and the host evaluation script (`eval.sh`). It enables self-contained validation of operator behavior, such as DNS reflection in airgapped scenarios, by booting k3s environments and verifying outcomes via structured JSON reports.

## Deep Dive
### Reporting Contract
The `Report` and `ReportPC` structs define the exact JSON schema expected by `run_scenario`. This ensures stable parsing via `jq` on the host side, preventing drift between Go-side serialization and shell-side queries. `WriteReportJSON` marshals results with consistent indentation to maintain compatibility across substrates.

### Namespace Resolution
`AcquireNamespace` resolves the target environment into a `Lab` struct, supporting dual-substrate execution. It prioritizes Firecracker hermetic mode (checking `FC_KUBECONFIG`) over live cluster mode (`KATE_TEST_CLUSTER`). This logic allows the same harness code to run in airgapped VMs without requiring a live lab0 dependency, enabling self-contained validation of in-cluster DNS reflection.

### Test Execution
The `Runner` interface allows scenarios to inject logic that returns pass/fail conditions given a context and lab configuration. `RunnerFunc` provides an adapter for simple functions, facilitating flexible test definition while maintaining a unified reporting structure.

## Key Interfaces
*   `Report`: Struct defining the overall scenario outcome.
*   `ReportPC`: Struct defining pass/fail status for individual conditions.
*   `Runner`: Interface for executing test scenarios and returning results.
*   `WriteReportJSON`: Function to marshal report data to disk.
*   `AcquireNamespace`: Function to resolve cluster and namespace from environment variables.

## Dependencies
*   `aquaduct.dev/weft-operator/internal/lab`
*   Standard library `encoding/json`, `os`, `context`

## Subcomponents
*   `BUILD.bazel`: Bazel build targets for library and tests.
*   `clusterharness.go`: Core implementation of types and helpers.
*   `clusterharness_test.go`: Unit tests for serialization and namespace acquisition.