<!-- docdaemon ts:2026-10-30T03:01:30Z hash:768e9c28b11769d7 model:openrouter/qwen/qwen3.7-flash -->
# e2e/install

## TL;DR
This directory contains the end-to-end test suite for validating Weft operator installation, CRD preflight logic, and Helm chart lifecycle management within isolated microVMs.

## Overview
The `install` package provides a comprehensive testing harness that runs against a k3s cluster hosted in a Firecracker microVM. It ensures the operator behaves correctly during initial deployment, handles missing dependencies gracefully, and survives schema changes during upgrades. By embedding Go binaries directly into the VM environment, these tests verify the full operator lifecycle without relying on external network access or complex CI orchestration.

## Deep Dive
### CRD Preflight Validation
The `crds_test` scenario verifies that the operator fails fast when required Gateway API CRDs are absent. It simulates a "no CRDs" state, confirms the operator crash-loops with a specific preflight error, and then applies standard CRDs to ensure recovery. This validates that the operator does not proceed to reconciliation until its dependencies are satisfied, preventing silent failures.

### Chart Lifecycle Management
The `chart_test` suite exercises the Helm chart installation process, including upgrade and uninstall paths. It checks for ownership enforcement, ensuring Helm rejects unowned CRDs and handles conflicts via `--force-conflicts`. The tests also validate automatic Vertical Pod Autoscaler (VPA) stack installation and verify that existing Custom Resource instances survive schema changes during upgrades.

### Operator Readiness and Stability
Beyond installation, the tests monitor operator stability over time. They confirm that the operator remains stable past cache sync timeouts, correctly renews leader election leases, and passes health probes. The suite also verifies that pod restarts do not trigger unnecessary resource rewrites, ensuring idempotent behavior during recovery scenarios.

## Key Interfaces
*   `main`: Entry point registering scenario handlers and delegating to the e2e test runner.
*   `crdsSteps`: Defines the sequence of assertions for CRD presence and operator readiness.
*   `chartSteps`: Manages state and assertions for Helm chart install, upgrade, and uninstall.
*   `applyYAML`: Helper for server-side applying YAML manifests to the cluster.
*   `waitLeader`: Utility to wait for the operator pod to acquire leadership.

## Dependencies
*   Kubernetes API libraries for resource manipulation.
*   Internal e2e utilities (`e2ekit`) for test orchestration.
*   Helm binary for chart templating and management.
*   Firecracker microVM environment for isolated execution.

## Subcomponents
*   `00-namespaces.yaml`: Namespace definitions for isolation.
*   `BUILD.bazel`: Bazel build targets for test binaries.
*   `chart-values.yaml`: Helm values for operator installation.
*   `chart_scenario.go`: Test logic for chart lifecycle.
*   `crds-values.yaml`: Helm values for CRD preflight tests.
*   `crds_scenario.go`: Test logic for CRD validation.
*   `main.go`: Test entry point.
*   `util.go`: Shared utility functions.