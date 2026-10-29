<!-- docdaemon ts:2026-10-29T20:35:47Z hash:fcb3f1d6bd0fece7 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Bazel build rule for packaging and deploying the Weft Operator Helm chart.

The `helm_chart` target named `weft_operator` packages the operator's Kubernetes manifests into a Helm chart artifact. It explicitly lists templates to include, ensuring that Custom Resource Definitions (CRDs) are managed as standard release resources rather than using Helm’s `crds/` directory. This approach allows CRD schema updates to propagate during upgrades, though it requires careful handling of resource policies in production to prevent accidental deletion on uninstall.

Key configuration details:
- **Images**: References the operator container image via `//cmd:weft_operator_image_push`.
- **Registry**: Pushes charts to `oci://ghcr.io/aquaduct-dev/charts`.
- **Templates**: Includes core RBAC resources, VPA configurations, webhook services, and generated CRDs from `//api/v1alpha1:generated.crds`.
- **Upgrade Options**: Configured to install into the `weft-system` namespace, creating it if necessary.

Historical context indicates this structure evolved to address memory management issues by integrating Vertical Pod Autoscaler (VPA) resources directly into the chart, and to simplify dependency management by dropping bundled Gateway API CRDs in favor of operator-side discovery.