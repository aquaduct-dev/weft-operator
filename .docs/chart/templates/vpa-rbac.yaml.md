<!-- docdaemon ts:2026-10-29T20:35:50Z hash:855b7305c8112708 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the RBAC resources required for the Vertical Pod Autoscaler (VPA) components, installed conditionally by the weft-operator.

The template is guarded by `weft-operator.installVpaController`, ensuring VPA ClusterRoles are only created if no existing VPA instance claims them. It retargets the standard VPA RBAC from `kube-system` to the current release namespace, allowing the operator and VPA controllers to coexist.

Key resources include:
- **ServiceAccounts**: `vpa-admission-controller`, `vpa-recommender`, and `vpa-updater`.
- **ClusterRoles**: Define permissions for metrics reading, pod/node interaction, status updates, checkpoint management, eviction, in-place updates, target reading, and admission control.
- **RoleBindings**: Bind roles to service accounts within the release namespace, including leader election locks for the updater and recommender.

This setup enables the weft-operator to manage its own memory usage via a VerticalPodAutoscaler, preventing OOM kills when watching large cluster-wide resources like Gateways and DNSRecords.