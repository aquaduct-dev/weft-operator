<!-- docdaemon ts:2026-10-29T20:38:33Z hash:9fa5f83740b76602 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory implements a generic reconciliation framework for managing resource lifecycles in the Weft Operator.

## Overview
The `resource` package provides a centralized helper to reduce boilerplate in controller logic by abstracting common CRUD patterns. It allows controllers to inject specific creation, update, and deletion callbacks while handling the decision logic for when those operations should occur. This structure supports the operator's need to ensure resources match their desired state efficiently.

## Deep Dive
### Reconciliation Logic
The core `Resource` function orchestrates the lifecycle of a managed entity based on its current existence and desired state. It evaluates four distinct scenarios: creating missing resources, updating stale ones, deleting unwanted entities, or performing no-op actions if the state is already correct. Each step logs progress via an injected logger, ensuring traceability during debugging. The function assumes all provided callbacks are non-nil, simplifying error handling within the loop.

### Configuration and Callbacks
Reconciliation behavior is driven by the `Options` struct, which bundles necessary context and mutators. Callers must provide evaluators like `Exists` and `ShouldExist` to determine status, along with `IsUpToDate` to check for drift. Mutating operations (`Create`, `Update`, `Delete`) return errors on failure, allowing the framework to halt processing immediately. This design decouples the reconciliation strategy from the specific resource implementation, promoting reuse across different controllers.

## Key Interfaces
- `Resource(opts Options) (changed bool, err error)` - Main reconciliation entry point.
- `Options` - Struct bundling callbacks, loggers, and state evaluators.

## Dependencies
- `aquaduct.dev/weft-operator/internal/resource` (self-import path)

## Subcomponents
- `BUILD.bazel` - Bazel build rules for the Go library.
- `resource.go` - Core reconciliation implementation.