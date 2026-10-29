<!-- docdaemon ts:2026-10-29T20:38:37Z hash:ebb94054aaeb9520 model:openrouter/qwen/qwen3.7-flash -->
# DESCRIPTION.md

## TL;DR
This directory contains the Helm chart templates that define the Kubernetes resources for the Weft operator, its dependencies, and optional components.

## Overview
The templates in this directory render the complete control plane for the Weft operator, including the main Deployment, ServiceAccount, RBAC bindings, and webhook services. It also conditionally installs the Vertical Pod Autoscaler (VPA) stack to manage operator memory scaling and defines GatewayClass resources for network traffic management. The structure ensures that generated artifacts like RBAC roles are kept separate from static templates while maintaining a unified build interface via Bazel.

## Deep Dive
### Core Operator Resources
`deployment.yaml` defines the primary `weft-operator` pod, injecting environment variables for image registries and enabling the cluster-wide DNS reflector feature. Security is enforced through read-only root filesystems and temporary certificate storage in an `emptyDir`. The `serviceaccount.yaml` and `rolebinding.yaml` files establish the necessary permissions, linking the operator's service account to the `manager-role` ClusterRole.

### Webhook and TLS Management
The `webhook-service.yaml` exposes the trust injection endpoint via a ClusterIP service, ensuring secure internal communication. TLS certificates for admission webhooks are managed in `vpa-certs.yaml`, which uses Helm functions to generate self-signed CA and server certificates dynamically, preventing rotation issues during upgrades.

### Vertical Pod Autoscaler Integration
To prevent OOM kills caused by large cluster watch sets, the chart bundles the VPA controller stack (`vpa-controllers.yaml`, `vpa-crd.yaml`, `vpa-rbac.yaml`). This includes vendored CRDs and deployments for the recommender, updater, and admission controller. The `vpa.yaml` template configures a memory-only VPA for the operator itself, running in `Off` mode to provide advisory recommendations without enforcing evictions on single-replica workloads.

### Gateway and Reflector Configuration
`gatewayclass.yaml` creates the `GatewayClass` resource required for the Weft Gateway controller, optionally referencing configuration parameters. Recent updates introduced support for `WeftLocalDNSReflector`, allowing the operator to keep public hostnames resolvable within the cluster, reducing hairpinning latency for bulk transfers.

## Key Interfaces
- `weft-operator.labels`: Helper for consistent metadata labeling across all resources.
- `weft-operator.fullname`: Helper for generating unique resource names based on the release.
- `installVpaController`: Conditional helper determining whether VPA resources should be rendered.
- `.Values.gatewayClass.create`: Boolean flag controlling GatewayClass instantiation.
- `.Values.verticalPodAutoscaler.*`: Configuration block for VPA settings and overrides.

## Dependencies
- Helm standard library functions (`genCA`, `genSignedCert`, `lookup`).
- Bazel build system for packaging YAML assets.
- Kubernetes API groups: `rbac.authorization.k8s.io`, `autoscaling.k8s.io`, `networking.k8s.io`.

## Subcomponents
- `BUILD.bazel`: Bazel targets for packaging chart templates.
- `_helpers.tpl`: Shared Helm template helpers.
- `deployment.yaml`: Main operator deployment manifest.
- `gatewayclass.yaml`: GatewayClass resource definition.
- `rolebinding.yaml`: ClusterRoleBinding for operator permissions.
- `serviceaccount.yaml`: Operator ServiceAccount definition.
- `vpa-certs.yaml`: TLS secret generation for webhooks.
- `vpa-controllers.yaml`: VPA component deployments and services.
- `vpa-crd.yaml`: VPA CustomResourceDefinitions.
- `vpa-rbac.yaml`: RBAC resources for VPA components.
- `vpa.yaml`: Operator-specific VerticalPodAutoscaler resource.
- `webhook-service.yaml`: Webhook exposure service.