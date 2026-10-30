<!-- docdaemon ts:2026-10-30T03:02:27Z hash:b1de98a480ab9f76 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the Helm chart for packaging, deploying, and managing the Weft Operator along with its optional VPA sidecars and Gateway integration.

## Overview
The chart defines the core Kubernetes resources required to run the Weft Operator, including RBAC policies, webhook infrastructure, and deployment manifests. It addresses historical stability issues by bundling a Vertical Pod Autoscaler (VPA) stack to manage memory scaling, preventing OOM kills during large-scale reconciliation. Additionally, it supports cluster-wide DNS reflection via `WeftLocalDNSReflector` to keep traffic within the cluster and integrates with Gateway API for traffic routing.

## Deep Dive
### Operator Core
The `deployment.yaml` configures the main controller with health probes, leader election, and TLS certificate storage in an `emptyDir`. RBAC is established through `serviceaccount.yaml` and `rolebinding.yaml`, granting cluster-wide permissions. A `webhook-service.yaml` exposes the trust injection endpoint securely. The chart uses `helm_chart` targets to ensure CRDs are managed by Helm during upgrades, avoiding silent schema pruning. Server-side apply conflicts from previous manual installs are handled via `--force-conflicts`.

### VPA Stack
To prevent OOM kills caused by large watch sets, the chart conditionally installs a VPA stack (`vpa-controllers.yaml`, `vpa-crd.yaml`, `vpa-rbac.yaml`) when `installVpaController` is true. This ensures multi-operator safety. The `vpa.yaml` resource applies memory-only autoscaling with `updateMode: Off`, providing advisory recommendations without mutating running pods, which preserves predictable resource allocation while respecting single-replica constraints. TLS certificates for the VPA admission webhook are generated in-chart using `genCA` and `genSignedCert`.

### Gateway Integration & DNS Reflection
The `gatewayclass.yaml` defines the `GatewayClass` linked to the Weft gateway controller, enabling automatic Gateway reconciliation and supporting optional parameter references to `WeftGateway` custom resources. By default, the operator creates a `WeftLocalDNSReflector` named `cluster-default` to serve published hostnames in-cluster, avoiding hairpinning through the bastion. This keeps bulk transfers, such as image pushes, efficient.

## Key Interfaces
- `weft-operator.fullname`: Helper for consistent resource naming.
- `weft-operator.labels`: Helper for standard metadata labels.
- `weft-operator.serviceAccountName`: Helper for ServiceAccount identification.
- `installVpaController`: Conditional helper for VPA component installation.

## Dependencies
- Helm built-in functions (`genCA`, `genSignedCert`, `lookup`).
- Kubernetes RBAC API group.
- Kubernetes Autoscaling API group (for VPA).
- Gateway API group (for GatewayClass).

## Subcomponents
- `_helpers.tpl`: Template helper functions.
- `BUILD.bazel`: Bazel build targets for packaging.
- `Chart.yaml`: Chart metadata and versioning.
- `deployment.yaml`: Main operator Deployment manifest.
- `gatewayclass.yaml`: GatewayClass resource definition.
- `rolebinding.yaml`: ClusterRoleBinding for operator permissions.
- `serviceaccount.yaml`: Operator ServiceAccount definition.
- `values.yaml`: Default configuration values.
- `vpa-certs.yaml`: TLS secret generation for VPA webhooks.
- `vpa-controllers.yaml`: VPA recommender, updater, and admission controller deployments.
- `vpa-crd.yaml`: VPA CustomResourceDefinitions.
- `vpa-rbac.yaml`: RBAC resources for VPA components.
- `vpa.yaml`: VerticalPodAutoscaler resource for the operator.
- `webhook-service.yaml`: Service exposing the trust injection webhook.