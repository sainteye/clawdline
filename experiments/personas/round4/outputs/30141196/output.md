## Summary

Fixed 5 real vulnerabilities in `server.go`:

1. **Path traversal (`GET /files`)** — the `HasPrefix(name, "..")` check only caught traversal at the start of the string, so `docs/../../secret/signing.key` escaped `files/` and read the sibling `secret/signing.key`. Fixed by rooting the path (`filepath.Clean("/"+name)`) before joining, the standard idiom that collapses any `..` against `/` instead of letting it climb out.
2. **IDOR on `GET /notes/{id}`** — any authenticated user could read any note by ID, including other users' private notes. Added the same owner-or-admin check that `PUT` already had.
3. **Missing auth on `DELETE /notes/{id}`** — had no `currentUser` check at all, so unauthenticated requests could delete any note. Added auth + owner-or-admin check.
4. **Mass assignment on `PUT /me`** — decoded the request body straight into the `User` struct, letting a user `PUT {"role":"admin"}` to self-promote. Now decodes into a `{Name, Email}`-only struct.
5. **Admin token check** — swapped the non-constant-time `!=` for `crypto/subtle.ConstantTimeCompare` (timing-attack hardening), and replaced the hardcoded `"dev-admin-token"` fallback in `main()` with a randomly generated token printed at startup.

Verified with throwaway tests exercising each exploit (traversal into `secret/signing.key`, cross-user note read, unauthenticated delete, self-promotion) — all now blocked — then removed the test file. `go build`/`go vet`/`go test` all pass.
