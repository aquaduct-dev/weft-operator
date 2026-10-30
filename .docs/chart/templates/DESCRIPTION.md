<!-- docdaemon ts:2026-10-30T03:01:08Z hash:daaf9e21daaf9c09 model:openrouter/qwen/qwen3.7-flash -->
# DESCRIPTION.md

## TL;DR
This directory contains the Helm chart templates for deploying the Weft operator, its VPA sidecars, and associated Kubernetes resources.

## Overview
The templates define the core operator deployment, RBAC policies, and webhook infrastructure required for cluster management. It also conditionally installs a Vertical Pod Autoscaler (VPA) stack to manage memory scaling, addressing historical OOM issues caused by large watch sets. Gateway API integration is handled via a dedicated `GatewayClass` resource.

## Deep Dive
### Operator Core
The `deployment.yaml` configures the main controller with health probes, leader election, and TLS certificate storage in an `emptyDir`. The `serviceaccount.yaml` and `rolebinding.yaml` establish the necessary permissions for cluster-wide resource management. A `webhook-service.yaml` exposes the trust injection endpoint securely within the cluster.

### VPA Stack
To prevent OOM kills during large-scale reconciliation, the chart bundles the VPA controller components (`vpa-controllers.yaml`, `vpa-crd.yaml`, `vpa-rbac.yaml`). These are installed only when `installVpaController` is true, ensuring multi-operator safety. The `vpa.yaml` resource applies memory-only autoscaling with `updateMode: Off`, providing advisory recommendations while respecting single-replica constraints. TLS certificates for the VPA admission webhook are generated in-chart using `genCA` and `genSignedCert` in `vpa-certs.yaml`.

### Gateway Integration
The `gatewayclass.yaml` defines the `GatewayClass` linked to the Weft gateway controller, allowing automatic Gateway reconciliation. It supports optional parameter references to `WeftGateway` custom resources.

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
- `deployment.yaml`: Main operator Deployment manifest.
- `gatewayclass.yaml`: GatewayClass resource definition.
- `rolebinding.yaml`: ClusterRoleBinding for operator permissions.
- `serviceaccount.yaml`: Operator ServiceAccount definition.
- `vpa-certs.yaml`: TLS secret generation for VPA webhooks.
- `vpa-controllers.yaml`: VPA recommender, updater, and admission controller deployments.
- `vpa-crd.yaml`: VPA CustomResourceDefinitions.
- `vpa-rbac.yaml`: RBAC resources for VPA components.
- `vpa.yaml`: VerticalPodAutoscaler resource for the operator.
- `webhook-service.yaml`: Service exposing the trust injection webhook.