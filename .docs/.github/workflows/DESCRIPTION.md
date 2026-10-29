<!-- docdaemon ts:2026-10-29T20:38:33Z hash:02f8ebb4312067b9 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This directory contains GitHub Actions workflows for continuous integration testing and release publishing.

## Overview
The workflows automate the build, test, and deployment pipeline using Bazel as the primary build system. The `test.yml` workflow validates code changes by running unit and integration tests while managing build caches to optimize performance. The `release.yml` workflow triggers on successful test runs to produce binary artifacts, container images, and Helm charts for distribution.

## Deep Dive
### Test Execution (`test.yml`)
Runs on `ubuntu-latest` with full Git history and LFS support. It installs Java 17 and Bazelisk v1.25.0 to resolve the `.bazelversion`. Tests are executed via `bazelisk test`, excluding targets tagged `requires-kvm` due to environment limitations. Logs are streamed in real-time, and the Bazel output base is cached between runs using run-number-based keys to ensure deterministic restoration.

### Release Publishing (`release.yml`)
Triggered only after `test.yml` succeeds on `main`. It restores the Bazel cache, then builds binaries for AMD64 and ARM64 architectures. A sanity check verifies binary architecture compatibility. The workflow authenticates with GitHub Container Registry (GHCR) using `GITHUB_TOKEN` to push multi-architecture images tagged with version and `latest`. It also publishes Helm charts, creates an annotated Git tag, and generates a GitHub Release containing the compiled binaries. Cache is saved post-build for subsequent runs.

## Key Interfaces
- Workflow dispatch events and branch filters
- Bazel build targets (`//cmd:cmd`, `//cmd:cmd_arm64`)
- GHCR authentication via `GITHUB_TOKEN`
- Git tag and Release creation actions

## Dependencies
- Bazelisk v1.25.0
- Java 17 (Temurin)
- GitHub Container Registry
- Git Large File Storage (LFS)

## Subcomponents
- `release.yml`
- `test.yml`