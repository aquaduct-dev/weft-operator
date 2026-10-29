<!-- docdaemon ts:2026-10-29T20:36:08Z hash:0929b26f73f6f776 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This Bazel build file defines the Go library, test suite, and public source group for the DNSRecord controller.

The `go_library` target builds `dnsrecord`, implementing the reconciler logic for `aquaduct.dev` domain registrations. It depends on the `v1alpha1` API types and the `aquaducttaas` controller to coordinate state. The library integrates with `controller-runtime` for Kubernetes watch loops and status management.

The `go_test` target runs unit tests using Ginkgo/Gomega and `envtest`. It configures a local etcd and API server via `KUBEBUILDER_ASSETS` to simulate a cluster environment without external dependencies. Test data includes generated CRDs to ensure schema validity during integration checks.

A critical optimization in the reconciler prevents unnecessary re-triggering of watches. By comparing sorted outputs (`ResolvedIPs`, `selectBastions`) against previous states, the controller only updates `.status` when actual changes occur. This avoids stamping `LastSyncTime` on every pass, which previously caused infinite reconcile loops across the fleet.