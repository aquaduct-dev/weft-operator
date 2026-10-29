<!-- docdaemon ts:2026-10-29T20:35:50Z hash:90115ebe11d6a327 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the CustomResourceDefinitions for VerticalPodAutoscaler and its checkpoints, vendored from the Kubernetes autoscaler project.

The CRDs are rendered only when `installVpaController` is true, ensuring they are installed solely by the bootstrapping instance. This conditional logic prevents conflicts in multi-operator environments.

The file includes two resources:
- **VerticalPodAutoscalerCheckpoint**: Stores internal VPA state for recommender recovery.
- **VerticalPodAutoscaler**: Configures automatic resource management based on utilization.

These definitions enable the operator to manage its own memory limits via a VPA, addressing OOM kills caused by large cluster watch sets. The VPA controller ensures these CRDs exist before the operator attempts to create VPA objects.