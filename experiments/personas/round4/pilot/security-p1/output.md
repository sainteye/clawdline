Clean: no leftover scratch file, tests pass, `gofmt` reports no issues.

## Summary

Fixed six issues in `server.go`:

1. **Path traversal (`GET /files`)** — the `strings.HasPrefix(name, "..")` check missed traversal like `docs/../../secret/signing.key`. Now anchors the requested path at `/` before `filepath.Clean`, joins with `filesDir`, and verifies the result is still inside `filesDir`.
2. **Privilege escalation via mass assignment (`PUT /me`)** — the request body was decoded directly into the `User` struct, letting anyone set `"role":"admin"`. Now decodes into a restricted `{name, email}` struct and only copies those fields.
3. **Missing auth on `DELETE /notes/{id}`** — had no auth check at all (unlike `GET`/`PUT`), so any unauthenticated request could delete any note. Added the same owner-or-admin check used by `PUT`.
4. **IDOR on `GET /notes/{id}`** — only checked that *some* valid token was present, not ownership, letting Bob read Alice's "private" note. Added an owner-or-admin check.
5. **Non-constant-time admin token check** — switched `!=` to `crypto/subtle.ConstantTimeCompare` to close a timing side-channel.
6. **Log injection in the `POST /notes` audit line** — user-supplied title was spliced unescaped into a log line; switched to `%q` so newlines/quotes can't forge log entries.

Verified with `go build`, `go vet`, `go test`, and targeted exploit attempts (traversal, role escalation, cross-user note read/delete) that now fail as expected while legitimate requests still succeed.
