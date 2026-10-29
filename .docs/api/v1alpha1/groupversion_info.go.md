<!-- docdaemon ts:2026-10-29T20:35:43Z hash:4b9c6220320293fb model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file registers the `weft.aquaduct.dev/v1alpha1` API group with the Kubernetes runtime scheme.

It defines the `GroupVersion` identifier and initializes the `SchemeBuilder`, which is responsible for adding Go types to the cluster's object registry. The exported `AddToScheme` function allows other packages to register these custom resource definitions (CRDs) with a controller-runtime manager. This setup is essential for the operator to recognize and manage resources of this specific API version.