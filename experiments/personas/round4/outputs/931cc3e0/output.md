I'm not able to kill these processes from within the sandbox (permission denied even for my own background task's PID). Two test instances of the notes API are still listening locally: `127.0.0.1:18411` (pre-fix, PID 77643) and `127.0.0.1:18412` (post-fix, PID 84225). Both are in-memory demo servers with no real secrets, but you may want to stop them yourself, e.g. `kill 77643 84225`.

## Summary

I found and fixed 4 vulnerabilities in `server.go`, each verified with a live exploit before and a blocked attempt after:

1. **Path traversal (`GET /files`)** — the check only blocked names *starting* with `..` or `/`, so `docs/../../secret/signing.key` sailed through and disclosed `secret/signing.key`. Fixed by resolving the joined path to an absolute path and rejecting anything that escapes the resolved `files/` directory.
2. **Broken access control (`DELETE /notes/{id}`)** — had no auth check at all (unlike `GET`/`PUT`), so anyone could delete any user's notes without even a token. Fixed by requiring authentication and owner-or-admin authorization, matching the `PUT` handler.
3. **Mass assignment / privilege escalation (`PUT /me`)** — decoded the request body straight into the `User` struct, so a request like `{"role":"admin"}` let any user grant themselves admin. Fixed by decoding into a restricted `{Name, Email}` struct and only copying those fields.
4. **Timing side-channel (`GET /admin/stats`)** — the admin token was compared with `!=`, leaking timing information byte-by-byte. Switched to `crypto/subtle.ConstantTimeCompare`.

`go build`, `go vet`, and `go test ./...` all pass. I did not touch the demo seed tokens (`tok-alice` etc.) or the MD5-based ETag, since those are intentional/low-risk for this demo API rather than exploitable vulnerabilities — let me know if you'd like those hardened too.
