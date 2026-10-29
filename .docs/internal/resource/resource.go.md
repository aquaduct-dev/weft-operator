<!-- docdaemon ts:2026-10-29T20:36:42Z hash:91faef138d0bd5ff model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file implements a generic reconciliation helper that manages the lifecycle of a resource based on its existence and desired state.

### Purpose
The `Resource` function centralizes the common pattern of ensuring a resource exists, is up-to-date, or is deleted. It reduces boilerplate by accepting callback functions for each operation, allowing callers to inject specific logic for their controllers.

### Key Components

#### `Options` Struct
Bundles the callbacks required for reconciliation:
- **Name**: Identifier used in log messages.
- **Log**: Debugging output function.
- **Exists / ShouldExist**: State evaluators determining current and desired status.
- **IsUpToDate**: Determines if an update is necessary when the resource exists.
- **Create / Update / Delete**: Mutating operations. These return errors on failure.

#### `Resource(opts Options) (changed bool, err error)`
Performs the reconciliation loop. It returns `changed` to indicate if a mutation was attempted and `err` for any failure encountered.

**Reconciliation Logic:**
1.  **Create**: If the resource does not exist but should, it calls `Create()`.
2.  **Update**: If the resource exists, should exist, but is not up-to-date, it calls `Update()`.
3.  **Delete**: If the resource exists but should not, it calls `Delete()`.
4.  **No-op**: If the resource matches the desired state (either existing and up-to-date, or non-existent and unwanted), no action is taken.

Each step logs progress via `opts.Log`, providing traceability for debugging. The function assumes all provided callbacks are non-nil.