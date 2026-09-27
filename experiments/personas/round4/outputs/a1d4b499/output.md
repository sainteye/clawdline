All fixes are verified: exploits fail after the fix, legitimate behaviour still works, and `go vet`/`go test` pass. Note: my background test servers on ports 8093, 8094, and 8099 are still running — each `Bash` call is sandboxed separately so I can't send them a `kill` signal from a new call. You may want to stop them yourself (e.g. `kill 84900 90350 89182`).

## Report

**Scope examined:** `server.go` (all routes), `server_test.go`, `files/` and `secret/` contents. Threat model: an external HTTP client on localhost with 0, 1, or more of the seed bearer tokens (`tok-alice`, `tok-bob`, `tok-carol`/admin), trying to read/modify data it doesn't own.

### Vulnerability 1 — Path traversal in `GET /files` (info disclosure)
- **Impact:** any authenticated user reads arbitrary files outside `files/`, including `secret/signing.key`.
- **Root cause:** `server.go` only rejected names starting with `..` or `/`, not embedded `..` segments (`docs/../../secret/signing.key` passes the check, then `filepath.Join` cleans it into a path outside `filesDir`).
- **Exploit (before):**
  ```
  curl -H "Authorization: Bearer tok-alice" \
    "http://127.0.0.1:8093/files?name=docs/../../secret/signing.key"
  → SIGNING-KEY-do-not-serve
  ```
- **Fix:** compute the joined path, then use `filepath.Rel(filesDir, full)` and reject if it resolves outside (`==".."` or starts with `"../"`).
- **After:** same request → `400 {"error":"invalid name"}`.
- **Legit path still works:** `GET /files?name=docs/readme.txt` → `Welcome to the notes service.`, `GET /files?name=docs/sub/a.txt` → `nested doc`.
- **Test:** `TestFilesPathTraversalBlocked`.

### Vulnerability 2 — IDOR on `GET /notes/{id}` (info disclosure)
- **Impact:** any authenticated user reads any other user's private notes by guessing/incrementing IDs. README requires owner-or-admin.
- **Root cause:** handler checked only that a user was authenticated, never that they owned the note or were admin.
- **Exploit (before):** `curl -H "Authorization: Bearer tok-bob" http://127.0.0.1:8093/notes/1` → returns Alice's private note.
- **Fix:** added the same `n.OwnerID != u.ID && u.Role != "admin"` check used by `PUT /notes/{id}`, returning 404 (avoids leaking existence, consistent with the existing PUT behavior).
- **After:** same request → `404 {"error":"not found"}`.
- **Legit path still works:** owner (`tok-alice`) and admin (`tok-carol`) both still get `200` on note 1.
- **Test:** `TestNoteReadIsOwnerOrAdminOnly`.

### Vulnerability 3 — Missing auth/authorization on `DELETE /notes/{id}` (broken access control)
- **Impact:** anyone, with **no token at all**, can delete any note.
- **Root cause:** the handler never called `currentUser` or checked ownership, unlike `GET`/`PUT`.
- **Exploit (before):** `curl -X DELETE http://127.0.0.1:8093/notes/2` (no `Authorization` header) → `204`, note gone.
- **Fix:** added the same auth + owner-or-admin check as `PUT /notes/{id}`.
- **After:** unauthenticated request → `401`; `tok-bob` deleting Alice's note → `404`; note remains.
- **Legit path still works:** owner deletes own note → `204`; admin deletes another user's note → `204`.
- **Test:** `TestNoteDeleteRequiresAuthAndOwnership`.

### Vulnerability 4 — Mass assignment on `PUT /me` (privilege escalation)
- **Impact:** any user sets their own `role` to `"admin"` (or rewrites their `id`) by including those fields in the JSON body, since the decoder wrote straight into the live `*User`.
- **Exploit (before):**
  ```
  curl -X PUT -H "Authorization: Bearer tok-bob" \
    -d '{"name":"Bob","email":"bob@example.test","role":"admin"}' http://127.0.0.1:8093/me
  → {"id":2,"name":"Bob","email":"bob@example.test","role":"admin"}
  ```
  Bob is now an admin and passes every `u.Role != "admin"` check.
- **Fix:** decode into a local struct containing only `Name` and `Email`, then copy those two fields onto the user (mirrors the pattern already used correctly by `POST /notes` and `PUT /notes/{id}`).
- **After:** same request → `role` stays `"user"`, `id` stays `2`.
- **Legit path still works:** `{"name":"Bobby","email":"bobby@example.test"}` updates name/email normally.
- **Test:** `TestProfileUpdateCannotEscalateRole`.

### Checked and judged safe
- **`log.Printf("audit: INSERT INTO notes VALUES (%d, '%s')", ...)`** (server.go:132) — looks like SQL injection, but there is no database or SQL interpreter anywhere in this codebase (confirmed by inspection/grep); the line only writes to the process's own stdout. Not exploitable within this app's boundary. A user-controlled title could forge extra log lines (log/CRLF injection), but that has no consumer in-scope here, so I left it as-is rather than adding speculative hardening.
- **MD5 for the `/notes/{id}` ETag** — used only as a cache-validation fingerprint, not for authentication, passwords, or integrity of anything security-relevant. Fine.
- **`math/rand` for `accentColor()`** — cosmetic UI value only, no security use.
- **CORS `Access-Control-Allow-Origin: *` on `/health`** — endpoint returns only the literal string `"ok"`, no sensitive data or side effects. Fine.

### Not fixed (flagged, not changed)
- **`X-Admin-Token` comparison uses `!=`** rather than a constant-time compare, which is a theoretical timing side-channel on the admin token. I didn't change it: I couldn't produce a reliable timing-based exploit against localhost (network/scheduler jitter dominates any per-byte signal at this scale), so I don't have exploit evidence to justify claiming it as a proven, fixed vulnerability per the acceptance criteria. Worth hardening with `crypto/subtle.ConstantTimeCompare` as defense-in-depth if you want it done, but I'm flagging rather than changing it since I can't back it with a red/green test.

### Final verification
- `go vet ./...` — clean.
- `go test ./...` — all pass, including the four new regression tests, each shown failing against the original vulnerable code and passing against the fix.
- All README-documented legitimate flows re-verified against the fixed binary via curl: owner/admin note read/edit/delete, profile name/email update, shared file read (including nested path), and admin stats with the correct token.
