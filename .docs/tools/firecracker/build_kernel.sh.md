<!-- docdaemon ts:2026-10-29T20:36:26Z hash:8930c96e694b972e model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This script builds an uncompressed vmlinux ELF from a pinned Linux source tarball and configuration, optimized for Firecracker microVMs.

### Purpose
The script automates the compilation of a minimal Linux kernel suitable for booting directly in Firecracker. It is designed to run within a Bazel `genrule` using the host toolchain (gcc, make, etc.), ensuring reproducible builds without requiring module signing keys or BTF debugging tools.

### Key Behaviors
- **Argument Parsing**: Accepts paths for the source tarball (`--tarball`), configuration file (`--config`), output location (`--out`), and parallel job count (`--jobs`). Defaults to `nproc` if jobs are unspecified.
- **Environment Setup**: Creates a temporary working directory that is automatically cleaned up on exit. Extracts the tarball and identifies the source root directory.
- **Configuration Handling**: Copies the provided `.config` into the source tree and runs `make olddefconfig`. This step fills in default values for any options missing from the pinned config due to version drift between the source and the configuration file.
- **Build Process**: Compiles the kernel using `make vmlinux`, producing the uncompressed ELF binary required by Firecracker.
- **Output**: Copies the resulting `vmlinux` to the specified output path.

### Integration
This script serves as the bridge between Bazel build rules and the actual kernel compilation process. By disabling `CONFIG_MODULES` and `CONFIG_DEBUG_INFO_BTF` in the input config, it ensures a fast, lightweight build that avoids dependencies on external tools like `pahole` or key management utilities.