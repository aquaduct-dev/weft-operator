<!-- docdaemon ts:2026-10-30T02:59:33Z hash:ebfa7f53fba61702 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the `crds` end-to-end test scenario, verifying operator behavior across Gateway API CRD installation states and lifecycle events.

The `crdsSteps` function defines a sequence of checks:
1. **Preflight Failure**: Confirms the operator crash-loops with a specific error when no Gateway API CRDs are present.
2. **Standard Channel**: Verifies readiness after applying standard CRDs, logs ignored route types (TCP/TLS/UDP), and ensures HTTP Gateways produce tunnels while remaining stable past cache sync timeouts.
3. **Lifecycle Checks**: Validates leader election lease renewal, health/readiness probes, and that pod restarts do not trigger unnecessary resource rewrites or generation bumps.
4. **Experimental Channel**: After restarting the operator with experimental CRDs, it confirms TCPRoute support and absence of "ignored" warnings.

Key helpers include `snapshot` for comparing deployment state before/after restarts, and `diagnostics` for collecting cluster state on failure.