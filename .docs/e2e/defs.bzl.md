<!-- docdaemon ts:2026-10-30T02:59:29Z hash:4ed29702029465e6 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file provides shared macros to configure and run Firecracker-based e2e tests for the weft-operator using k3s.

### Purpose
The module centralizes the setup of ephemeral VMs used for end-to-end testing. It preloads common container images (pause, CoreDNS, local-path provisioner, operator, weft) and essential files into every test environment via `COMMON_IMAGES` and `COMMON_FILES`.

### Key Functions

**`e2e_binary_image(name, binary, tag)`**
Constructs an OCI image layer containing a static Go binary at `/e2e`. It builds the tarball, creates the image based on Alpine, loads it into the local registry, and exposes the resulting tarball as a target. This allows scenario binaries to be injected directly into the guest without network pulls.

**`weft_e2e_test(...)`**
Wraps `firecracker_k3s_test` to standardize e2e execution. It automatically merges `COMMON_IMAGES` and `COMMON_FILES` with any user-provided extras. If no custom scenario manifest is provided in `extra_manifests`, it generates a default ConfigMap YAML specifying the scenario name. The test binary receives the path to this manifest via command-line arguments.

### Configuration
Tests are executed using `bazel test --config=kvm`. Resource limits (vCPUs, memory, disk size) and timeouts are configurable parameters passed through to the underlying Firecracker rule.