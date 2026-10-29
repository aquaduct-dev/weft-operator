<!-- docdaemon ts:2026-10-29T20:36:52Z hash:594213c9137ebc8f model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script serves as the PID 1 init process for Firecracker microVMs in the networking self-test suite, verifying end-to-end connectivity between paired VMs.

It mounts `/proc` and `/sys`, then parses its own IP address and a peer’s IP from the kernel command line (`ip=` and `ofd.peer=` parameters). After ensuring the loopback interface is up, it attempts to ping the peer up to 15 times with a one-second timeout.

The script outputs `NETOK <self>-><peer>` if the ping succeeds, or `NETFAIL <self>-><peer>` otherwise. This marker allows the orchestrator to verify that the multi-VM substrate correctly routes traffic across the shared bridge. Upon completion, the VM powers off.