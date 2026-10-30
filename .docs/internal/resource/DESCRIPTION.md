<!-- docdaemon ts:2026-10-30T03:01:06Z hash:6f985f6111275bb9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `internal/resource` package provides generic reconciliation logic and image policy utilities for managing Kubernetes resources.

## Overview
This directory centralizes common patterns for ensuring resource existence, updates, and deletion through a callback-based framework. It also standardizes container image selection and pull policies to support both online and air-gapped environments. By abstracting these concerns, controllers can focus on domain-specific logic while maintaining consistent lifecycle management.

## Deep Dive
### Reconciliation Framework
The `Resource` function implements a generic CRUD loop that accepts callbacks for existence checks and mutations. It evaluates whether a resource should exist, is up-to-date, or needs deletion, executing the appropriate action. This reduces boilerplate in controllers by handling the state machine logic, logging, and error propagation uniformly. The framework assumes non-nil callbacks and returns a boolean indicating if any mutation occurred.

### Image Policy Management
Helpers in this package determine how Kubernetes pods reference container images. `WeftImageOr` provides a fallback default image when none is specified. `PullPolicy` analyzes image references to decide between `Always`, `IfNotPresent`, or other policies. Digests and pinned tags use `IfNotPresent` to optimize for offline scenarios, while floating tags like `:latest` trigger `Always` pulls to ensure freshness. This distinction supports idempotency and reliable updates in air-gapped deployments.

## Key Interfaces
- `Resource(opts Options) (changed bool, err error)` - Generic reconciliation loop
- `Options` - Struct bundling callbacks for name, logging, state evaluation, and mutations
- `WeftImageOr(image string) string` - Returns image or default fallback
- `PullPolicy(image string) corev1.PullPolicy` - Determines pull strategy based on tag type

## Dependencies
- `aquaduct.dev/weft-operator/internal/resource` (self)
- `@io_k8s_api//core/v1:core` (Kubernetes API types)

## Subcomponents
- `BUILD.bazel` - Bazel build rules
- `image.go` - Image helper functions
- `image_test.go` - Unit tests for image logic
- `resource.go` - Reconciliation framework