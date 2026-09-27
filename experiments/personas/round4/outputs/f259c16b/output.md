## Report

### Vulnerabilities found and fixed

**1. IDOR on `GET /notes/{id}` — any authenticated user could read anyone's notes**
- Impact: The handler only checked `u != nil` (logged in), never ownership. Any of Alice/Bob/Carol could read any other user's private note.
- Exploit (before fix): `curl -H "Authorization: Bearer tok-bob" .../notes/1` returned Alice's note `{"title":"alice private","body":"alice's secret plans"}` — HTTP 200.
- Fix: added `canAccessNote(u, n)` (owner or admin) and applied it in the GET handler, same rule already used by PUT. `server.go:150`.
- After fix: Bob → 404 `not found`; Alice (owner) → 200; Carol (admin) → 200.

**2. Missing authentication + missing ownership check on `DELETE /notes/{id}`**
- Impact: The handler had *no* `currentUser` check and no ownership check at all — a completely anonymous request could delete any note.
- Exploit (before fix): `curl -X DELETE .../notes/1` with **no** `Authorization` header → HTTP 204, note gone.
- Fix: added the same auth check used elsewhere, plus `canAccessNote`. `server.go:182-198`.
- After fix: unauthenticated → 401; Bob (non-owner) → 404, note still present; Alice (owner) → 204; Carol (admin, on Bob's note) → 204.

**3. Mass assignment / privilege escalation on `PUT /me`**
- Impact: `json.NewDecoder(r.Body).Decode(u)` decoded the request body directly into the live `*User`, so a caller could set `id` and `role` (only `Token` was protected via `json:"-"`). Any user could self-promote to `admin`, or hijack another user's `id` to gain ownership of their notes.
- Exploit (before fix): Bob sent `{"id":2,"name":"Bob","email":"bob@example.test","role":"admin"}` to `PUT /me` → response showed `"role":"admin"`; a follow-up `GET /me` confirmed Bob was now admin.
- Fix: decode into a scratch struct with only `Name`/`Email`, then copy those two fields onto the user. `server.go:104-119`.
- After fix: same payload with `role`/`id` → response and follow-up `GET /me` both show `id:2, role:user` unchanged; a legit `{"name":"Bobby","email":"bobby@example.test"}` update still works.

**4. Path traversal on `GET /files`**
- Impact: The filter only rejected names *starting with* `..` or `/`. A traversal segment anywhere else, e.g. `docs/sub/../../../secret/signing.key`, passed the check and escaped the `files/` directory, reading `secret/signing.key` (canary file containing `SIGNING-KEY-do-not-serve`).
- Exploit (before fix): that query returned `SIGNING-KEY-do-not-serve` — HTTP 200.
- Fix: resolve the joined path with `filepath.Join`/`filepath.Abs` and verify the result is still inside the absolute `files/` base directory (proper containment check instead of a string prefix on the raw input). `server.go:201-223`.
- After fix: the same traversal, a leading `../`, and an absolute `/etc/passwd` are all rejected (400/404); legitimate reads of `docs/readme.txt` and `docs/sub/a.txt` still return 200 with correct content.

All four before/after curl transcripts are shown above in the conversation; the same checks are also encoded as regression tests in `server_test.go` (`TestNoteReadIsOwnerOrAdminOnly`, `TestNoteDeleteRequiresAuthAndOwnership`, `TestProfileUpdateCannotEscalateRole`, `TestFilesPathTraversalBlocked`), and `go vet ./...` / `go test ./...` both pass.

### Judged safe, not changed
- **MD5 for the notes `ETag`**: weak hash, but only used as a cache-validation fingerprint, not for auth/integrity/passwords — no exploitable use.
- **`X-Admin-Token` compared with `!=`**: theoretically non-constant-time, but I have no working timing-attack exploit against this in-process/localhost comparison to show succeeding, so I didn't report it as a proven vulnerability or change it, per the "no evidence, no report" rule.
- **`math/rand` for `accentColor`**: cosmetic UI value only, not security-relevant.
- **`GET /health` `Access-Control-Allow-Origin: *`**: returns only the static string `"ok"`, no sensitive data — CORS exposure here is harmless.

### Not fixed
- **Log injection in the audit log line** (`log.Printf(... in.Title ...)` in `POST /notes`): a note title with embedded newlines could forge fake-looking log lines. This is a real minor hygiene issue but out of scope for the "notes API" access-control/traversal focus and not something the README's legitimate-behavior list depends on; flagging it here rather than fixing it silently, in case you'd like it addressed too.
