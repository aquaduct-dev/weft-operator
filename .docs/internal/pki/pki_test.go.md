<!-- docdaemon ts:2026-10-29T20:36:40Z hash:62f734dafe67ffc1 model:openrouter/qwen/qwen3.7-flash -->
## TL;DR
This file contains unit tests for the PKI package, verifying certificate issuance, validation, and renewal logic.

The tests ensure that `NewCA` generates valid CA certificates and `IssueLeaf` creates leaf certificates signed by them. It validates that leaf keys load correctly into TLS structures and verify against specific DNS names while rejecting unauthorized hosts. The test suite covers edge cases such as case-insensitive hostname matching and order independence in SAN lists.

Key verification points include:
- **Verification**: Ensures `leaf.Verify` succeeds for listed hosts and fails for unlisted ones using a custom root pool.
- **Renewal Logic**: Tests `RenewalReason` to confirm it triggers when hosts are missing, added, inside the renewal window, or signed by a different CA.
- **Error Handling**: Confirms that issuing a leaf with no hostnames returns an error.
- **Utilities**: Validates `Fingerprint` produces distinct, short hashes and `CountCerts` accurately parses PEM blocks from mixed content.