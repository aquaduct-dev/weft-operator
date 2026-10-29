<!-- docdaemon ts:2026-10-29T20:36:09Z hash:59be85789927cac2 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `NodeProber`'s logic to clean up orphaned WeftServer resources.

The test suite validates the `cleanupOrphanedServers` method, ensuring that WeftServer instances referencing non-existent Kubernetes nodes are automatically deleted. It uses a fake client to simulate various scenarios: servers linked to existing nodes (preserved), servers linked to missing nodes (deleted), and servers without the required node label (preserved). This ensures resource hygiene by removing stale configurations when their underlying infrastructure is gone.