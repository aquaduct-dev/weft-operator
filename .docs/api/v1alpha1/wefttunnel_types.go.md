<!-- docdaemon ts:2026-10-29T20:35:44Z hash:e33117d4828ad41a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes Custom Resource Definition (CRD) types for the WeftTunnel API, specifying its structure and status.

It declares `WeftTunnel` as a root object with subresource support for status updates. The `Spec` struct configures tunnel behavior via `TargetServers`, `SrcURL`, and `DstURL`. The `Status` struct tracks operational state using standard Kubernetes `Conditions`.

The `WeftTunnelList` type provides a collection wrapper for multiple instances. Finally, the `init` function registers these types with the global Scheme builder, enabling serialization and deserialization within the controller runtime environment.