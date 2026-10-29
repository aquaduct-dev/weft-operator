<!-- docdaemon ts:2026-10-29T20:39:53Z hash:8ba988b1fb606acb model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory serves as the root configuration and entry point for the Weft Operator, defining build rules, CI pipelines, and deployment manifests.

## Overview
The repository root orchestrates the project’s lifecycle by integrating Bazel-based builds with GitHub Actions for continuous integration and release. It establishes the canonical source of truth for Kubernetes Custom Resource Definitions (CRDs) and provides Helm charts for production deployment. The structure ensures that code generation, testing, and packaging are reproducible across environments while maintaining strict security boundaries for secrets and credentials.

## Deep Dive
### Build and Module Configuration
`MODULE.bazel` and `BUILD.bazel` define the workspace structure, pinning Go SDK 1.25.0 and external dependencies like `controller-runtime`. Gazelle is configured to auto-generate build rules, ensuring consistency between source files and Bazel targets. `tools.go` acts as a dependency sink for build-time tools like `controller-gen`, preventing runtime bloat.

### CI/CD Pipelines
The `.github/` directory contains workflows for testing and releasing. `test.yml` validates code integrity using Bazel, caching output bases for performance. `release.yml` triggers on main branch merges, building multi-architecture binaries and publishing container images to GHCR alongside Helm charts.

### Deployment and Security
The `chart/` directory packages the operator into a Helm chart, managing RBAC, webhooks, and Vertical Pod Autoscaler (VPA) integration. Security is enforced via read-only filesystems and dynamic TLS certificate generation for admission webhooks. Secrets are never stored in CRDs; instead, resources reference Kubernetes Secrets for authentication tokens and connection strings.

## Key Interfaces
- `AddToScheme`: Registers all v1alpha1 API types with the controller-runtime scheme.
- `main()`: Entry point for the operator binary and node agent.
- `setup-pre-commit-hook.sh`: Automates local development validation.
- `WeftServerReconciler`: Provisions and monitors bastion server resources.
- `WeftLocalDNSReflector`: Manages in-cluster DNS reflection and trust injection.

## Dependencies
- Bazelisk v1.25.0
- Java 17 (Temurin)
- Go 1.25.0
- Kubernetes client-go and controller-runtime
- GitHub Container Registry (GHCR)

## Subcomponents
- `.github/`: CI/CD workflows.
- `api/`: CRD type definitions.
- `chart/`: Helm chart templates.
- `cmd/`: Application entry point and build rules.
- `e2e/`: End-to-end test suite.
- `internal/`: Core controllers and utilities.
- `scripts/`: Development automation scripts.
- `tools/`: Build and test infrastructure.