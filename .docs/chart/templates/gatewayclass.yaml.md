<!-- docdaemon ts:2026-10-29T20:35:47Z hash:2f4be0d719774685 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the `GatewayClass` resource required for the Weft Gateway controller to reconcile Gateways.

The template conditionally renders a `GatewayClass` object when `.Values.gatewayClass.create` is true. It sets the `controllerName` to `weft.aquaduct.dev/gateway-controller`, which links this class to the specific reconciler instance. This prevents reconciliation failures caused by missing GatewayClass definitions, ensuring that any Gateway referencing this class can be processed correctly.

If `.Values.gatewayClass.parametersRef` contains both `name` and `namespace`, the template includes a `parametersRef` block. This references a `WeftGateway` custom resource of group `weft.aquaduct.dev`, allowing the GatewayClass to point to its configuration source. The metadata labels are inherited from the shared `weft-operator.labels` helper to maintain consistency across resources.