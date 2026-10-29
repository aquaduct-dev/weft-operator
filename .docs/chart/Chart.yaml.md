<!-- docdaemon ts:2026-10-29T20:35:45Z hash:22532380d8aafca2 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the metadata and versioning for the Weft Operator Helm chart.

It serves as the primary manifest for the `weft-operator` application, identifying it as a standard Helm v2 chart of type `application`. The `version` field tracks the chart package release, while `appVersion` specifies the corresponding operator binary version, both currently set to `0.1.0`. This separation allows independent versioning of the packaging structure from the underlying software artifact.