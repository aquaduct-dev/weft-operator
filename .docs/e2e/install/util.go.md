<!-- docdaemon ts:2026-10-30T02:59:32Z hash:7915aa77b32c0d34 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides utility functions for end-to-end testing of the Weft operator, focusing on Kubernetes resource management and cluster interaction.

It defines constants for namespaces and labels used across tests. Key helpers include `applyYAML` for server-side applying YAML manifests, and lifecycle utilities like `operatorPod`, `operatorReadyOther`, and `waitLeader` to manage and verify the operator's state during leader election. It also includes `waitLog` and `logLines` for verifying log output, `helm` for executing Helm commands within the test environment, and specific helpers like `gatewayTunnel` and `waitGatewayTunnel` to validate Gateway API integration.