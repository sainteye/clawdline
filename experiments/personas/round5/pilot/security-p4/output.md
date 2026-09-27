Implemented a security hardening pass across the API.

Key fixes:

- Enforced exact `Bearer` authentication and constant-time admin/hook secret comparisons.
- Prevented profile mass assignment of IDs, roles, and tokens.
- Added ownership checks for notes, search, and integrations.
- Forced imported notes and new integrations to belong to the authenticated user.
- Prevented imports from replacing existing notes.
- Added 1 MiB JSON limits, trailing-data rejection, and unknown-field rejection.
- Secured file access against traversal and escaping symlinks.
- Added SSRF protection for integration tests, including DNS results and redirects.
- Limited integration response bodies to 1 MiB.
- Removed integration secrets from API responses and logs.
- Added hook nonce replay protection.
- Escaped user-controlled audit-log fields.
- Replaced MD5 ETags with SHA-256.
- Removed the insecure default admin token; `ADMIN_TOKEN` is now required.

Changed files:

- [server.go](<workdir>/server.go)
- [integrations.go](<workdir>/integrations.go)
- [server_test.go](<workdir>/server_test.go)

Verification passed:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
