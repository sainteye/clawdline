Everything is clean. Here's the summary report.

## Vulnerabilities found and fixed

### 1. `DELETE /notes/{id}` — missing authentication/authorization (broken access control)
**Impact:** anyone, including unauthenticated requests, could delete *any* user's note.
**Exploit (before fix):**
```
$ curl -H "Authorization: Bearer tok-alice" .../notes/1
{"id":1,"owner_id":1,"title":"alice private","body":"alice's secret plans"}
$ curl -X DELETE .../notes/1          # no Authorization header at all
HTTP 204
$ curl -H "Authorization: Bearer tok-alice" .../notes/1
{"error":"not found"}
```
**Fix:** added the same `currentUser`/owner-or-admin check used by `PUT /notes/{id}` (server.go:178-194).
**After fix:** unauthenticated delete → `401`; non-owner delete → `404`; owner delete → `204`; admin delete of someone else's note → `204` (all verified above).

### 2. `GET /notes/{id}` — IDOR
**Impact:** any authenticated user could read any other user's private note by guessing/incrementing the id.
**Exploit (before fix):** Alice (`tok-alice`) fetched Bob's note 2 (`bob private / bob's diary`) directly.
**Fix:** added the owner-or-admin check (server.go:136-153), matching README's "owner, or an admin" rule.
**After fix:** non-owner → `404`; owner → `200`; admin → `200` (verified above).

### 3. `PUT /me` — mass assignment / privilege escalation
**Impact:** the handler decoded the request body straight into the live `*User` struct, so a caller could set `role` (and `id`) as well as name/email. A normal user could self-promote to `admin`, which then bypassed the owner check on `PUT`/`DELETE /notes/{id}`.
**Exploit (before fix):**
```
$ curl -X PUT -H "Authorization: Bearer tok-alice" -d '{"name":"Alice","email":"...","role":"admin"}' .../me
{"id":1,...,"role":"admin"}
$ curl -X PUT -H "Authorization: Bearer tok-alice" -d '{"title":"pwned",...}' .../notes/2   # Bob's note
{"id":2,"owner_id":2,"title":"pwned","body":"owned by alice"}
```
**Fix:** decode into a `struct{ Name, Email string }` and only assign those two fields (server.go:100-115); `Role`/`ID` can no longer be touched by the client.
**After fix:** role/id in the response stay `"user"`/`1` even when the client sends `role`/`id`; a subsequent `PUT` on another user's note still returns `404`. Legit name/email update still works and is reflected on `GET /me`.

### 4. `GET /files?name=...` — path traversal
**Impact:** the check `strings.HasPrefix(name, "..")` only blocked traversal at the very start of the string, so `docs/../../secret/signing.key` bypassed it (`filepath.Join` still collapses the embedded `..`), leaking `secret/signing.key` outside `files/`.
**Exploit (before fix):**
```
$ curl -H "Authorization: Bearer tok-alice" ".../files?name=docs/../../secret/signing.key"
SIGNING-KEY-do-not-serve
```
**Fix:** clamp the requested name under a virtual root before joining — `filepath.Clean("/" + name)` collapses any `..` against the leading `/` instead of letting it escape, then `filepath.Join(filesDir, clean)` (server.go:196-212).
**After fix:** the same traversal, plus `../secret/signing.key` and `/etc/passwd`, all return `404`; legit reads of `docs/readme.txt` and `docs/sub/a.txt` still return `200` with correct content.

## Judged safe (no fix)
- **`GET /health` sends `Access-Control-Allow-Origin: *`**: returns only a static `"ok"`, no auth/cookies involved, so permissive CORS here discloses nothing.
- **MD5 used for the notes `ETag`**: it's a cache-validation checksum, not used for auth/integrity of a secret, so collision weaknesses in MD5 aren't relevant.
- **`math/rand` for `accentColor()`**: explicitly cosmetic (comment says so), not used for tokens/security.
- **`X-Admin-Token` compared with `!=` instead of a constant-time comparison**: theoretically a timing side channel, but I couldn't produce a working timing exploit over HTTP (network/goroutine/GC jitter dwarfs any per-byte signal for a short token), so per the "no evidence, no report" rule I left it as-is rather than claim it as a fixed vulnerability.

## Not fixed
- **Audit log line** (`log.Printf("audit: INSERT INTO notes VALUES (%d, '%s')", ...)`) embeds the user-supplied title unescaped. This can forge extra log lines (CRLF injection into `log.Printf` output) if a title contains newlines, but the comment confirms nothing reads this back into a database or parser, so I found no way to turn it into a demonstrable exploit — flagging it here rather than fixing speculatively, since it's cosmetic log hygiene rather than a proven vulnerability.

## Verification
```
$ go vet ./...
$ go test ./...
ok  	notesapi	0.224s
```
`server_test.go` now also has regression tests (`TestDeleteNoteRequiresOwnerOrAdmin`, `TestGetNoteBlocksOtherUsers`, `TestUpdateProfileCannotEscalateRole`, `TestFilesPathTraversalBlocked`) covering each fix plus the legitimate owner/admin paths, in addition to the manual curl evidence above against a real `go run`-equivalent binary on `127.0.0.1`.
