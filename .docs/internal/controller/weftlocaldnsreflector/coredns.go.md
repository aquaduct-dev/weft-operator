<!-- docdaemon ts:2026-10-29T20:36:15Z hash:2c6a7d48d559be93 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file manages the CoreDNS configuration for in-cluster hostname resolution via a WeftLocalDNSReflector.

The `renderCoreDNS` function generates valid CoreDNS server blocks, mapping specific hostnames to a service IP. It enforces strict DNS name validation and ensures the output is safe for CoreDNS ingestion, preventing cluster-wide outages from malformed configurations.

The `syncCoreDNS` method on the `Reconciler` applies this rendered content to the shared CoreDNS ConfigMap. It uses an uncached API reader to bypass manager caching limitations for shared resources. The logic handles creating new ConfigMaps, updating existing ones with new overrides, or deleting keys when content is empty. Crucially, it relies on resource version checks during updates to prevent clobbering concurrent changes to other keys within the same ConfigMap.