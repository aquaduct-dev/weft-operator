<!-- docdaemon ts:2026-10-29T20:36:14Z hash:69ab36704372c091 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements the Kubernetes controller that reconciles Gateway API resources into WeftTunnels and DNSRecords.

The `WeftGatewayReconciler` watches Gateways, classes, and routes to maintain desired state. It validates ownership via `GatewayClass`, then iterates over attached HTTP, TCP, TLS, and UDP routes to create or update `WeftTunnel` CRs mapping internal services to external listeners. A finalizer ensures cleanup of associated `DNSRecord` objects upon Gateway deletion.

Key logic includes:
- **Route Attachment**: Strictly matches routes to specific listeners using `isRouteAttachedToListener` to prevent cross-product tunnel creation.
- **L4 Pinning**: For TCP/UDP protocols, hostnames are pinned to a single bastion (`pickL4Bastion`) to avoid port collisions, whereas L7 traffic fans out to all eligible bastions.
- **Header Rewriting**: Translates `RequestHeaderModifier` filters into URL fragments for tunnel configuration.
- **DNS Management**: Automatically creates and prunes `DNSRecord` CRs in the AquaductTaaS namespace based on listener hostnames.