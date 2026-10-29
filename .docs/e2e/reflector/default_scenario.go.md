<!-- docdaemon ts:2026-10-29T20:35:55Z hash:e9bc9e07844c013d model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the E2E test scenario for validating the operator's automatic creation and lifecycle management of a cluster-wide default DNS reflector.

The `defaultReflectorSteps` function orchestrates five sequential checks: installing the operator with default values, verifying the creation of the `cluster-default` reflector (which uses `allHostnames` mode to serve all tunnelled registries), deploying a backend registry, confirming in-cluster resolution via a trusted certificate, and finally ensuring that disabling the feature (`defaultReflector.enabled=false`) correctly removes the reflector, its CoreDNS override, and node-level host entries.

Key functions include:
- **`checkDefaultCreated`**: Waits for the reflector resource and its associated CA secret to appear, ensuring pods are admitted securely.
- **`checkDefaultReflects`**: Verifies the reflector is ready and actively serving the expected hostname, then probes connectivity using the service IP.
- **`checkDefaultDisabled`**: Applies a disabled manifest, waits for cleanup of resources, and confirms the hostname no longer resolves.