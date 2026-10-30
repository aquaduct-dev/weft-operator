<!-- docdaemon ts:2026-10-30T02:59:25Z hash:c6892b443c4ea31c model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file defines the `scenario` struct and its lifecycle steps for end-to-end testing the AquaductTaaS operator against an in-cluster fake API.

It orchestrates the installation of the operator, deployment of a mock `aquaduct.dev` service, and a real Weft server (bastion-a). The `steps()` method returns a sequence of checks verifying TaaS synchronization, token caching/re-exchange logic, DNSRecord registration states, and Gateway AutoDNS fan-out behavior. Helper methods like `checkMirrored` validate that the operator correctly creates and manages `WeftServer` resources based on upstream bastion availability.