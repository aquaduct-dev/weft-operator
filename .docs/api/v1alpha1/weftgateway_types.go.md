<!-- docdaemon ts:2026-10-29T20:35:43Z hash:12b3aadd4a1befbc model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes Custom Resource Definition (CRD) types for the WeftGateway API.

It declares `WeftGateway` and `WeftGatewayList`, registering them with the controller-runtime scheme in `init()`. The `Spec` struct allows specifying a list of `TargetServers` to route tunnels to; if empty, tunnels connect to all available servers. The `Status` struct tracks operational state via a slice of `metav1.Condition`.

Kubebuilder markers (`+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`) generate the necessary API machinery and enable status subresource updates. This resource serves as a parameter reference for `GatewayClass` to configure Weft-specific gateway settings.