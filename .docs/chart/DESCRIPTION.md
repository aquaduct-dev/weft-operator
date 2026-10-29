<!-- docdaemon ts:2026-10-29T20:39:38Z hash:44117cfab00f77aa model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains the Helm chart definition, templates, and configuration values for deploying the Weft Operator.

## Overview
The `chart` directory packages the Weft Operator into a standard Helm v2 application chart, managing the lifecycle of its Kubernetes resources. It handles core operator components, including RBAC, webhooks, and deployment manifests, while integrating optional features like Vertical Pod Autoscaler (VPA) and Gateway API support. The structure ensures that Custom Resource Definitions (CRDs) are managed as release resources to facilitate schema updates during upgrades.

## Deep Dive
### Core Operator Resources
The `deployment.yaml` template defines the primary `weft-operator` pod, injecting environment variables for image registries and enabling cluster-wide DNS reflection. Security is enforced via read-only root filesystems and temporary certificate storage in an `emptyDir`. Service accounts and role bindings establish necessary permissions, linking the operator to the `manager-role` ClusterRole.

### Webhook and TLS Management
Webhook services are exposed via a ClusterIP service in `webhook-service.yaml` to ensure secure internal communication. TLS certificates for admission webhooks are dynamically generated using Helm functions in `vpa-certs.yaml`, creating self-signed CA and server certificates to prevent rotation issues during upgrades.

### Vertical Pod Autoscaler Integration
To mitigate OOM kills caused by large cluster watch sets, the chart bundles the VPA controller stack (`vpa-controllers.yaml`, `vpa-crd.yaml`, `vpa-rbac.yaml`). This includes vendored CRDs and deployments for the recommender, updater, and admission controller. The `vpa.yaml` template configures a memory-only VPA for the operator itself, running in `Off` mode to provide advisory recommendations without enforcing evictions on single-replica workloads.

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
- `Chart.yaml`: Chart metadata and versioning.
- `deployment.yaml`: Main operator deployment manifest.
- `gatewayclass.yaml`: GatewayClass resource definition.
- `rolebinding.yaml`: ClusterRoleBinding for operator permissions.
- `serviceaccount.yaml`: Operator ServiceAccount definition.
- `vpa-certs.yaml`: TLS secret generation for webhooks.
- `vpa-controllers.yaml`: VPA component deployments and services.
- `vpa-crd.yaml`: VPA CustomResourceDefinitions.
- `vpa-rbac.yaml`: RBAC resources for VPA components.
- `vpa.yaml`: Operator-specific VerticalPodAutoscaler resource.
- `values.yaml`: Default configuration values.
- `webhook-service.yaml`: Webhook exposure service.