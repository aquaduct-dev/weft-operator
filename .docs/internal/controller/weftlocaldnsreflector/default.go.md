<!-- docdaemon ts:2026-10-29T20:36:16Z hash:8a6fe5f035add4cc model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the operator-managed cluster-default WeftLocalDNSReflector, ensuring it exists or is removed based on configuration.

The `DefaultReflector` struct acts as a leader-only runnable that periodically syncs the reflector state. It uses the `DefaultReflectorLabel` to distinguish its managed resources from user-created ones with the same name.

Key behaviors in `Sync`:
- **Deletion**: If `Enabled` is false and the reflector is owned by the operator, it deletes it.
- **Ownership Check**: If a reflector with the target name exists but lacks the default label, it is ignored to preserve hand-made configurations.
- **Creation/Update**: If not found, it creates the reflector with `AllHostnames` (if no specific hostnames are set) or the sorted unique list of provided hostnames. If found and owned, it updates only the hostname selection fields, preserving other administrator-tuned spec fields.

The logic resides in the operator rather than Helm templates because Helm cannot create Custom Resources in the same release that installs their CRDs. The periodic re-check ensures resilience against manual deletion.