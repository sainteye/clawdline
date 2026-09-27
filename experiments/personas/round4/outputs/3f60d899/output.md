## Report

### Vulnerabilities found and fixed

**1. `DELETE /notes/{id}` — missing authentication and authorization (Critical)**
`server.go` had no `currentUser` call and no ownership check at all — literally anyone, without a token, could delete any note.
- Exploit (before fix, no `Authorization` header):
  ```
  curl -X DELETE http://127.0.0.1:8099/notes/2   → 204 No Content
  curl http://127.0.0.1:8099/notes/2 -H "Authorization: Bearer tok-bob"  → 404 (Bob's note is gone)
  ```
- Fix: require `currentUser`, then require `n.OwnerID == u.ID || u.Role == "admin"`, mirroring `PUT`.
- After fix: anonymous delete → `401`; non-owner delete → `404`; owner delete → `204` (still works); admin delete of someone else's note → `204` (still works). See `TestDeleteNoteRequiresAuthAndOwnership`.

**2. `GET /notes/{id}` — IDOR / broken access control (High)**
Any authenticated user could read any other user's note; only `PUT` had the ownership check, contradicting the README ("owner, or an admin").
- Exploit (before): `curl /notes/1 -H "Authorization: Bearer tok-bob"` → returns Alice's private note.
- Fix: added the same `n.OwnerID != u.ID && u.Role != "admin"` guard used by `PUT`.
- After: Bob → `404`; Alice (owner) → `200`; Carol (admin) → `200`. See `TestGetNoteBlocksNonOwners`.

**3. `PUT /me` — mass assignment → privilege escalation (Critical)**
The handler decoded the request body straight into the live `*User` pointer. `Role` and `ID` have no `json:"-"` tag, so any user could set `"role":"admin"` and become an admin (or clone another user's `id`).
- Exploit (before): Bob PUTs `{"role":"admin", ...}` → his role becomes `admin`, then he successfully `DELETE`s Alice's note.
- Fix: decode into a `struct{ Name, Email string }` DTO and copy only those two fields onto the user.
- After: same payload → role stays `user`; Bob still can't touch Alice's note (`404`); legitimate name/email updates still return `200` with the new values. See `TestUpdateProfileCannotEscalate`.

**4. `GET /files` — path traversal (High)**
The filter only checked `strings.HasPrefix(name, "..")`/`"/"`, which a segment like `docs/../../secret/signing.key` doesn't start with, but `filepath.Join` still collapses it outside `filesDir` (verified: `filepath.Join("files","docs/../../secret/signing.key") == "secret/signing.key"`).
- Exploit (before): `curl "/files?name=docs/../../secret/signing.key"` → returned the contents of `secret/signing.key` (`SIGNING-KEY-do-not-serve`), a file that sits outside `files/`.
- Fix: `filepath.Clean("/" + name)` first, anchoring the path to a virtual root that `Clean` never lets `..` escape, then join with `filesDir`.
- After: the same traversal payloads (and `docs/../../../../../../etc/passwd`) → `404`; legitimate `docs/readme.txt` and nested `docs/sub/a.txt` still return their contents. See `TestFilesPathTraversalBlocked`.

### Additional hardening (not headline findings, fixed anyway)

- **`X-Admin-Token` comparison** used `!=` (non-constant-time). Switched to `crypto/subtle.ConstantTimeCompare`. I did not attempt a timing-attack PoC — over real HTTP on localhost, scheduling/network jitter dominates any nanosecond-scale timing signal for a short token, so I can't produce reliable exploit evidence either way. Applying the constant-time compare is free and standard practice, so I fixed it as defense-in-depth without listing it as a proven vulnerability. Right/wrong token behavior (200/403) is unchanged and covered by `TestAdminStatsToken`.
- **Audit log injection** in `POST /notes`: the title was interpolated with `%s` into a log line, so a title containing `\n` could forge a fake extra log entry (e.g. a bogus "admin login" line). Confirmed before/after with a title of `"x\nWARN forged admin login from 192.0.2.1"`:
  - Before: log showed two lines, the second looking like an independent log entry.
  - After (`%q` instead of `%s`): a single escaped line, `audit: INSERT INTO notes VALUES (3, "x\nWARN forged admin login from 192.0.2.1")`.

### Checked and judged safe (no change)

- **MD5 for the notes `ETag`**: non-cryptographic cache-validation use, not used for integrity/auth — collision weakness is irrelevant here.
- **The `log.Printf("... INSERT INTO notes ...")` line itself**: looks like SQL, but there's no database and no query execution anywhere in the code — the comment confirms it's just a fake audit line, so there's no SQL injection.
- **`math/rand` for `accentColor()`**: cosmetic UI value only, never used for tokens/secrets.
- **CORS `Access-Control-Allow-Origin: *` on `/health`**: the endpoint returns only the static string `"ok"`, no session/credentials, so wildcard CORS leaks nothing.
- **Content sniffing on `GET /files` output**: no endpoint lets any user write into `files/`, so there's no attacker-controlled content an attacker could get served with a browser-chosen MIME type.

### Not examined / out of scope

- Rate limiting or brute-forcing of bearer tokens — the task's threat model didn't ask me to redesign the auth scheme, and tokens are fixed seed values here.
- TLS/transport security — instructed to stay on localhost.

### Test evidence

`go vet ./...` and `go test ./...` both pass, including 5 new regression tests (`TestGetNoteBlocksNonOwners`, `TestDeleteNoteRequiresAuthAndOwnership`, `TestUpdateProfileCannotEscalate`, `TestFilesPathTraversalBlocked`, `TestAdminStatsToken`) that encode both the blocked exploits and the legitimate paths (owner/admin read & delete, profile name/email update, shared-doc reads, valid admin token).

One environment note: this sandbox wouldn't let me send `kill` to background `go run` processes (including one pre-existing, unrelated process already bound to `:8082` before I started), so I ran my before/after servers on `:8099`/`:8100` instead — those two `go run` instances are still alive as orphaned background jobs since I couldn't terminate them; they're harmless (localhost only, in-memory state) but you may want to stop them yourself.
