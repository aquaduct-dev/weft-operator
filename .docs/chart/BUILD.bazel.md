<!-- docdaemon ts:2026-10-30T02:59:53Z hash:61b7d94b09ec6d8a model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rule for packaging and deploying the Weft Operator Helm chart.

The `helm_chart` target `weft_operator` configures how the operator is built, published to the OCI registry (`oci://ghcr.io/aquaduct-dev/charts`), and installed into Kubernetes clusters. It explicitly lists all template files and CRDs that constitute the release, ensuring they are bundled together.

Key configuration details:
- **CRD Management**: CRDs are included in the `templates` list rather than the `crds/` directory. This ensures they are managed by Helm during upgrades, preventing silent schema pruning on existing clusters. Note that this means `helm uninstall` will delete CRDs, which is acceptable for dev environments but requires `helm.sh/resource-policy: keep` annotation for production stability.
- **Upgrade Strategy**: The `upgrade_opts` include `--force-conflicts` to handle server-side apply conflicts that arise when transitioning from manual `kubectl` installs to Helm-managed releases. It also forces installation in the `weft-system` namespace.
- **Dependencies**: The chart depends on the operator image pushed via `//cmd:weft_operator_image_push`.