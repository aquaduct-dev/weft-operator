<!-- docdaemon ts:2026-10-29T20:36:00Z hash:53a313642cc72359 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This YAML configuration disables the operator's default `WeftLocalDNSReflector` to prevent automatic cluster-wide DNS reflection.

### Purpose
The file defines the `defaultReflector` settings for the Weft operator. It explicitly sets `enabled: false`, which instructs the operator not to create or maintain a cluster-default reflector.

### Context and Behavior
Historically, the operator was updated to automatically provision a `cluster-default` reflector in `allHostnames` mode. This default behavior ensures that traffic to Weft-published services remains within the cluster without requiring per-cluster setup.

By setting this value to `false`, administrators override that default. The operator will delete any existing cluster-default reflector it created, effectively turning off the global DNS reflection feature. This is useful when specific, manual reflector configurations are preferred over the automated cluster-wide approach.