<!-- docdaemon ts:2026-10-29T20:35:48Z hash:8d3691e9ce67bd56 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the ClusterRoleBinding that grants the operator's ServiceAccount permissions to manage cluster-wide resources.

The template renders a `ClusterRoleBinding` resource, linking the `manager-role` ClusterRole to the operator's ServiceAccount. This binding ensures the controller manager has the necessary RBAC permissions to function across the entire cluster.

Key components include:
- **Name**: Dynamically generated using the `weft-operator.fullname` helper, suffixed with `-manager-role-binding`.
- **Labels**: Applied via the `weft-operator.labels` helper to maintain consistency with other operator resources.
- **Role Reference**: Points to the `manager-role` ClusterRole within the `rbac.authorization.k8s.io` API group.
- **Subjects**: Specifies the operator's ServiceAccount as the subject, identified by the `weft-operator.serviceAccountName` helper and scoped to the current release namespace.