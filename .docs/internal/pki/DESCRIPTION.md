<!-- docdaemon ts:2026-10-29T20:38:34Z hash:35b91ae0f330b110 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
The `internal/pki` package provides cryptographic utilities for managing X.509 certificate lifecycles within the cluster.

## Overview
This directory implements the core PKI machinery required by the WeftLocalDNSReflector and trust injection webhook. It ensures that internal services communicate securely by generating, signing, and validating certificates before they expire. The package encapsulates complex TLS operations to provide a simple interface for certificate issuance and renewal checks.

## Deep Dive
### Certificate Generation and Signing
The package exposes `NewCA` to create self-signed ECDSA P-256 root certificates valid for ten years. These CAs serve as the trust anchor for issuing leaf certificates via `IssueLeaf`. This function enforces strict DNS name requirements, deduplicating and sorting Subject Alternative Names (SANs) to ensure deterministic output regardless of input order. A clock skew buffer is applied during validation to prevent rejection due to minor time discrepancies between nodes.

### Renewal and Validation
`RenewalReason` determines if a serving certificate requires reissuance by validating the signature chain against the current CA and checking SAN consistency. It flags certificates entering a 30-day renewal window or those signed by an outdated CA. This logic supports the WeftLocalDNSReflector’s need to maintain secure hairpinning traffic from pods to bastions and back through WireGuard tunnels without interruption.

### Utilities
Helper functions like `Fingerprint` generate stable hex digests of PEM data, enabling pod rollouts when certificates change. `CountCerts` parses PEM bundles to verify trust bundle integrity. All key material is handled as PEM bytes and never logged to prevent secret exposure.

## Key Interfaces
*   `NewCA`: Generates a fresh self-signed ECDSA P-256 CA certificate and private key in PEM format.
*   `IssueLeaf`: Signs a serving certificate using an existing CA with strict SAN enforcement.
*   `RenewalReason`: Determines if a serving certificate needs reissuance based on validity and expiration.
*   `Fingerprint`: Computes a stable 16-character hex digest of PEM data for change detection.
*   `CountCerts`: Parses a PEM bundle to count valid certificate blocks.

## Dependencies
*   Go standard library (`crypto/x509`, `crypto/ecdsa`, `encoding/pem`)
*   Bazel build system

## Subcomponents
*   `pki.go`: Core implementation of certificate lifecycle operations.
*   `pki_test.go`: Unit tests for issuance, validation, and renewal logic.
*   `BUILD.bazel`: Bazel build targets for source aggregation and library compilation.