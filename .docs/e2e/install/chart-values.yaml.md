<!-- docdaemon ts:2026-10-30T02:59:31Z hash:78cbdaf79781c255 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides the Helm values configuration for installing the operator and its dependencies in the e2e test environment.

It overrides default image references to use preloaded `:e2e` tags, ensuring tests run without network pulls. The `image.url` points to the `weft-operator` and `weftImage` to the `weft` component, both configured with `pullPolicy: Never`. This setup supports the chart_test VM's isolated execution model.

The configuration explicitly avoids overriding VPA or reflector settings, relying on chart defaults (including 1.7.0 tags) to maintain consistency with the preloaded environment. This minimalism ensures that tests focus on operator behavior rather than infrastructure variability.