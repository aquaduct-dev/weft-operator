<!-- docdaemon ts:2026-10-30T02:59:41Z hash:fad90cf4b75f99bf model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides helper functions to construct Kubernetes resources and verify the operational state of WeftTunnel and WeftServer objects during end-to-end testing.

It defines builders for `WeftServer`, `WeftTunnel`, and test deployments, generating deterministic names based on route hashes. The core verification logic resides in `checkTunnelDeployment` and `checkServerWorkloads`, which assert that the operator correctly creates underlying Deployments, Services, and PVCs with expected labels, owner references, and container arguments. These checks ensure the controller reconciles custom resources into valid workload specifications.

Wait-for helpers like `tunnelAvailable` and `serverAvailable` poll the API server to confirm status conditions reach desired states, enabling reliable synchronization in test scenarios. Utility functions such as `logPods` aid in debugging by dumping pod statuses and logs when failures occur.