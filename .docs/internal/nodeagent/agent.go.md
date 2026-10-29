<!-- docdaemon ts:2026-10-29T20:36:28Z hash:e637ccd3c9beffb3 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `node-agent` subcommand that configures node-level DNS and TLS trust to route specific hostnames through an in-cluster proxy.

The agent runs as a DaemonSet, periodically verifying connectivity to the reflector Service via TLS handshake. On success, it injects hostname mappings into `/etc/hosts` and registers CA certificates in `containerd`'s `certs.d` directory. If verification fails or the process terminates, it reverts all changes to ensure traffic falls back to the public internet rather than failing silently.

### Key Components

*   **`Config` & `parseFlags`**: Defines command-line arguments for the reflector name, service IP, target hostnames, CA bundle path, and system paths (`/etc/hosts`, `certs.d`). It validates required fields like `--reflector` and `--service-ip`.
*   **`Agent`**: The core loop controller. It holds configuration and logging state, managing the periodic health check cycle.
*   **`Run`**: Starts the main loop with signal handling (SIGINT/SIGTERM). It ensures `Cleanup()` is called on exit to remove injected state.
*   **`Tick`**: The primary reconciliation step. It reads the CA bundle, updates containerd trust files, applies host trust bundles, checks proxy health, and conditionally writes the `/etc/hosts` block only if the proxy is verified.
*   **`applyHosts`**: Safely rewrites the bind-mounted `/etc/hosts` file in place to avoid EBUSY errors, adding or removing the reflector's hostname entries based on proxy health.
*   **`applyHostTrust`**: Appends the reflector's CA to distro-specific CA bundles (e.g., `/etc/ssl/certs/ca-certificates.crt`) so host processes trust the proxy.
*   **`pruneCertsD`** & **`Cleanup`**: Remove stale or all managed entries from `containerd`'s certificate directories and revert hosts/trust changes when the agent stops.