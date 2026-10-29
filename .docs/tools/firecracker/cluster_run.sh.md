<!-- docdaemon ts:2026-10-29T20:36:27Z hash:cadf7e8d6b9264a0 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script orchestrates a cluster of real Firecracker microVMs on a shared host bridge for end-to-end testing.

It creates a Linux bridge and one user-owned TAP interface per node, allowing Firecracker to access the network without `CAP_NET_ADMIN`. Each node is booted via a specified runner with unique networking parameters (IP, MAC, TAP device) passed through environment variables. The script supports optional persistent data disks (`--persist-dir`) and custom boot images (`--boot-disk-dir`). It waits for all VMs to exit or for a timeout, collecting logs in a temporary directory, and ensures cleanup of all network interfaces and the bridge upon completion.