<!-- docdaemon ts:2026-10-29T20:35:51Z hash:a6961ad41dd6f6e9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines a VerticalPodAutoscaler (VPA) resource to manage memory scaling for the weft-operator deployment.

The VPA is configured to monitor and recommend memory usage only, leaving CPU requests and limits static as defined in `.Values.resources`. This separation exists because the operator runs as a single replica on clusters where the shared VPA updater enforces a minimum of two replicas, preventing automatic eviction required by `Auto` or `Recreate` modes. Consequently, the `updateMode` defaults to `Off`, making recommendations advisory rather than enforceable.

The template includes a guard clause checking for the `autoscaling.k8s.io/v1/VerticalPodAutoscaler` API version. This ensures the Custom Resource is only created after the VPA controller stack (which installs the CRD) has been successfully applied, avoiding ordering errors during initial Helm upgrades.

Configuration values are sourced from `.Values.verticalPodAutoscaler`, including:
- `enabled`: Toggles the VPA creation.
- `updateMode`: Controls how recommendations are applied (default `Off`).
- `minAllowedMemory` / `maxAllowedMemory`: Bounds for the recommended memory range.