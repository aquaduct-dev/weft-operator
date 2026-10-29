<!-- docdaemon ts:2026-10-29T20:36:52Z hash:59022686b5ee3480 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script generates a Bazel `sh_test` wrapper that executes a Firecracker microVM running k3s, captures serial output, and validates test results from embedded JSON reports.

### Purpose
The file acts as a code generator for integration tests involving Kubernetes on Firecracker. Unlike standard test generators, it handles result parsing by extracting JSON payloads delimited by specific markers (`===REPORT_BEGIN===`/`===REPORT_END===`) from the VM's serial log, rather than relying on exit codes or expect markers.

### Key Behaviors
- **Argument Parsing**: Accepts `--out`, `--runner-rloc`, and `--timeout` to configure the generated test script.
- **Runtime Environment**: The generated script resolves the runner binary via Bazel runfiles and sets up a temporary directory for outputs.
- **Execution & Logging**: Launches the microVM runner with a hard timeout cap. It streams serial logs to stderr in real-time using `tail -F` and captures them to a temp file.
- **Graceful Shutdown**: Monitors for `K3S_POWEROFF` signals to allow graceful shutdown before enforcing hard kill deadlines.
- **Report Extraction**: Uses `awk` to isolate the JSON report between markers, cleans whitespace/CR characters, and validates it with `jq`.
- **Validation**: Checks for a `pass_conditions` array in the report. If any condition has `passed != true`, the test fails and prints details to stderr.
- **Output Handling**: Saves the final report to `TEST_UNDECLARED_OUTPUTS_DIR` (persistent across runs) and optionally mirrors it to `SCENARIO_REPORT_PATH` if writable.