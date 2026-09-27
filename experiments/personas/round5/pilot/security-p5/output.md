Security review completed and confirmed vulnerabilities were fixed.

Key fixes:

- Enforced exact `Bearer` authentication.
- Added owner/admin authorization across notes and integrations.
- Prevented profile role/ID mass assignment.
- Prevented imports from selecting owners or overwriting records.
- Added strict 1 MiB JSON limits and trailing-data rejection.
- Blocked path traversal and symlink escapes.
- Replaced prefix secret checks with constant-time exact comparisons.
- Added webhook nonce replay protection.
- Prevented integration-secret exposure and logging.
- Hardened outbound integration tests against SSRF, redirects, and oversized responses.
- Added HTTP server timeouts.
- Removed the committed signing key and ignored future key files.
- Required `ADMIN_TOKEN` at startup instead of using a default.

Changes are in [server.go](<workdir>/server.go:97), [integrations.go](<workdir>/integrations.go:40), and expanded regression tests in [server_test.go](<workdir>/server_test.go:50).

Validation passed:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`

If the removed signing key was ever used outside this sample, revoke and rotate it. The documented seed authentication tokens should also never be deployed to production.
