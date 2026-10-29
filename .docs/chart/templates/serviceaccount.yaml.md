<!-- docdaemon ts:2026-10-29T20:35:48Z hash:f9a584cc84f6ce9b model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the Kubernetes ServiceAccount resource for the weft-operator, managed via Helm templates.

The template renders a `ServiceAccount` object with dynamic metadata. The `name` is derived from the `weft-operator.serviceAccountName` helper, ensuring consistency across other resources in the chart. Standard labels are applied using the `weft-operator.labels` helper to maintain uniform identification.

Conditional logic handles annotations: if `.Values.serviceAccount.annotations` is defined, they are merged into the metadata block. A conditional block checks `.Values.serviceAccount.create`, but currently contains no content, implying that creation control or additional configuration may be intended for future implementation or handled elsewhere. This file relies on standard Helm include functions to ensure proper indentation and formatting of nested YAML structures.