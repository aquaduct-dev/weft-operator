<!-- docdaemon ts:2026-10-29T20:36:10Z hash:371f9d47853f2eb0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the `classifyListenerHostnames` and `pickL4Bastion` helper functions in the Weft Gateway controller.

The tests verify that listener hostnames are correctly classified into Layer 7 (HTTP/HTTPS/TLS) or Layer 4 (TCP/UDP) categories, ensuring L4 listeners trigger a pessimistic classification when mixed with L7 protocols on the same hostname. It also validates that L4 bastion selection is deterministic, order-independent, and respects filtering rules such as skipping suspended bastions or those without IPs.