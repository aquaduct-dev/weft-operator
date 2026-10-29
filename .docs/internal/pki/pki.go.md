<!-- docdaemon ts:2026-10-29T20:36:31Z hash:e9b8a3161d56dabd model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This package manages X.509 certificate lifecycle operations, including CA generation, leaf signing, and renewal validation for internal cluster services.

### Purpose
The `pki` package provides the cryptographic machinery required by the WeftLocalDNSReflector controller and the trust injection webhook. It ensures that serving certificates are valid, correctly signed, and renewed before expiration to maintain secure communication within the cluster.

### Key Functions

*   **NewCA**: Generates a fresh self-signed ECDSA P-256 CA certificate and private key in PEM format. The CA is valid for 10 years and includes basic constraints for certificate signing.
*   **IssueLeaf**: Signs a serving certificate using an existing CA. It enforces strict DNS name requirements, sorts and deduplicates Subject Alternative Names (SANs), and applies a clock skew buffer to prevent rejection due to minor time discrepancies.
*   **RenewalReason**: Determines if a serving certificate needs reissuance. It validates the signature chain against the current CA, checks SAN consistency, and flags certificates entering the 30-day renewal window.
*   **Fingerprint**: Computes a stable 16-character hex digest of PEM data, used in annotations to trigger pod rollouts when certificates change.
*   **CountCerts**: Parses a PEM bundle to count valid certificate blocks, useful for verifying trust bundles.

### Design Notes
Key material is always returned as PEM bytes and never logged. The package uses deterministic sorting for DNS names to ensure consistent certificate issuance regardless of input order.