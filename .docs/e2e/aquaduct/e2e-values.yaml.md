<!-- docdaemon ts:2026-10-30T02:59:24Z hash:09109749af9d195f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file configures the Helm chart for the Aquaduct end-to-end microVM test environment.

It defines preloaded container images (`weft-operator` and `weft`) with `pullPolicy: Never` to ensure tests run against cached artifacts without network access. The configuration enables a `GatewayClass` to facilitate AutoDNS path testing and disables VerticalPodAutoscaler components to maintain a stable, minimal test footprint.

Authentication is routed through an in-cluster fake service via the `AUTHZ_ENDPOINT` environment variable, pointing to `fake-authz.aquaduct-fake.svc.cluster.local`. This setup supports the scenario binary's "fake" mode, which mimics real domain handlers and JWT validation logic using in-memory state.