<!-- docdaemon ts:2026-10-30T02:59:29Z hash:0c7d773ed91d3ee4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides the guest-side harness for running end-to-end Kubernetes scenarios inside a Firecracker microVM.

The `e2ekit` package orchestrates test execution by defining `Step`s and `Scenarios`. The `Main` function parses command-line flags, initializes a `Kit` to connect to the k3s cluster, and runs the selected scenario steps sequentially. A failure in any step halts execution, triggers diagnostic dumps (pod logs, events), and writes a JSON report via `clusterharness`.

Key components include:
- **`Kit`**: Holds Kubernetes clients (`client.Client`, `*kubernetes.Clientset`) and configuration. It provides helper methods like `Kubectl`, `Apply`, and `Delete` to interact with the cluster.
- **`WaitFor` & `Holds`**: Utility functions for polling until a condition is met or persists, essential for waiting on resource availability or stability.
- **Operator Lifecycle**: `InstallOperator` manages the deployment of the Weft operator, including CRD application and webhook readiness checks.
- **Diagnostics**: `DumpDiagnostics` collects pod states, warning events, and log tails to aid debugging when a step fails.
- **Node Inspection**: Functions like `ContainerPID` and `EstablishedConns` allow direct inspection of container processes and network state from within the VM.