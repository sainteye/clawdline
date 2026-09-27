Implemented a security hardening pass for the API.

Key fixes in [server.go](<workdir>/server.go):

- Enforced exact `Bearer <token>` authentication.
- Prevented cross-user note reading, searching, and deletion.
- Blocked profile mass-assignment and admin-role escalation.
- Made imports assign fresh IDs and the authenticated owner.
- Protected file access against traversal and symlink escapes.
- Replaced partial admin-token matching with constant-time comparison.
- Enforced the 1 MiB JSON limit, rejected trailing JSON and unknown fields.
- Escaped audit-log newlines to prevent log injection.
- Added secure content types and `X-Content-Type-Options: nosniff`.
- Added HTTP read/write/header timeouts.
- Removed the default admin token; `ADMIN_TOKEN` is now required.
- Replaced MD5 ETags with SHA-256 and fixed concurrent response races.

Added regression coverage in [server_test.go](<workdir>/server_test.go).

Validation passed:

```text
go test -race ./...   ok
go vet ./...          clean
```

The seeded bearer tokens remain intentionally documented demo credentials. They should be replaced with a real identity/token store before exposing this API publicly.
