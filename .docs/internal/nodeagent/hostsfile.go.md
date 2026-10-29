<!-- docdaemon ts:2026-10-29T20:36:28Z hash:ee1ab7b1cee0c6e4 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file manages the insertion, replacement, and removal of tagged blocks in `/etc/hosts` and CA bundle files.

The `nodeagent` package uses this logic to ensure that specific reflectors own distinct sections of shared system files without corrupting existing content. It relies on unique marker lines (`# BEGIN ...` and `# END ...`) to identify managed regions.

### Key Functions

*   **`withHostsBlock`**: Updates the hosts file for a given reflector. It sets the IP mapping for provided hostnames or removes the block entirely if no hostnames are specified.
*   **`withCABlock`**: Updates the CA bundle for a reflector. It appends PEM-encoded certificates within the managed block or removes the block if the input is empty.
*   **`withManagedBlock`**: The core engine that performs the actual file manipulation. It splits the content into lines, locates any existing block matching the `begin` and `end` markers, and replaces it with the new `body`. If no block exists, it appends the new one at the end.

### Implementation Details

The parser handles edge cases where a `BEGIN` marker lacks a corresponding `END` marker by treating only the marker line as part of the managed block, leaving subsequent lines intact. This prevents accidental deletion of unmanaged data during partial updates. All generated blocks include a "do not edit" comment to signal to humans and other tools that the section is automated.