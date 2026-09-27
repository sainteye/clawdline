## Summary

**Fixed (each with a regression test in `security_test.go` that fails on the old code, passes on the new):**

1. **Path traversal → arbitrary file read** (`server.go:200-205`) — `GET /files` only rejected names *starting* with `..`/`/`, so `docs/../../secret/signing.key` slipped past the check and read the signing key outside `files/`. Fixed by rooting the name at `/` before `filepath.Clean`, which collapses any escaping `..` regardless of position.
2. **Mass assignment → privilege escalation** (`server.go:108-114`) — `PUT /me` decoded JSON straight into the live `*User`, so `{"role":"admin"}` made any user an admin. Fixed by decoding into a `Name`/`Email`-only struct and copying just those fields across.
3. **Unauthenticated note deletion** (`server.go:178-194`) — `DELETE /notes/{id}` had no auth check at all. Added the same auth + ownership/admin check used by `PUT`.
4. **IDOR on note read** (`server.go:136-153`) — `GET /notes/{id}` only checked *some* valid token, not ownership, letting any user read anyone's private note. Added the ownership/admin check.
5. **Default admin token + timing side channel** (`server.go:216-221`, `main()`) — removed the hardcoded `"dev-admin-token"` fallback (now `log.Fatal`s if `ADMIN_TOKEN` isn't set) and switched the comparison to `crypto/subtle.ConstantTimeCompare`.

**Checked and sound:** `PUT /notes/{id}` already enforced ownership/admin correctly; `Token` field is properly excluded from JSON via `json:"-"`; CORS wildcard is scoped to the harmless `/health` route only.

**Not fixed (flagged, low severity, left as-is unless you want it addressed):** `POST /notes` formats `in.Title` into a log line styled like SQL (`server.go:132`) — not real SQL injection (no DB), but unsanitized input in a log line can forge/break log entries if the title contains newlines or quotes.

All tests pass (`go test ./...`), `go vet` is clean. README updated since `ADMIN_TOKEN` is now mandatory.
