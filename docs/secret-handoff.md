# Secret Handoff

Secret Handoff is the person-to-Session path for a password, credential, or API key needed by a local tool. The Note is an invitation and status surface, never the value's storage. A normal Session message and its transcript are also unsuitable: they retain what was sent. The receiving agent names the credential in the Note summary, shown as the label immediately above the dedicated multi-line input. The summary is a non-secret label, never the value.

## Contract

1. The Note identifies a live target conversation and the purpose of the requested secret. The request must not contain a secret value. Only the person may submit the value.
2. The dedicated input does not copy the value into the Session composer, a draft, a browser URL, browser storage, a notification, or a Note field. Its request travels through the existing authenticated local HTTP path or Cloud's end-to-end encrypted action channel. Cloud may queue ciphertext but must never see the plaintext.
3. The machine holds the value only in memory with a short deadline. It returns an opaque receipt and status, never the value, to the browser. A successful submission marks the Note handled immediately, in the same transaction as its durable receipt. Reloading keeps the Note handled. Completion does not consume the value: the target Session can still claim it once until a restart or deadline discards it. A rejected submission leaves the Note pending. A completed Note rejects new submissions; request a fresh Note when another handoff is needed.
4. The receiving live Session retrieves the value once through a local command that feeds it to a tool on standard input. The command opens a short-lived loopback receiver with a random nonce; the daemon sends the value to that receiver. No HTTP response contains the value, and the command never prints it. A different Session, a stale Session, a browser read, and a repeated claim are refused.
5. The value and any fingerprint derived from its content or length are excluded from durable receipts. The value is also excluded from audit events, request logs, and diagnostic error strings. Limits and error codes mention size or state, never content. The agent must keep the tool's own output and logs from repeating the value.

## Submission receipts and retries

The submission route uses a receipt for the **handoff operation**, not for equality of secret values. Its digest covers the HTTP method, exact conversation/Note path and the fixed `secret-handoff-operation-v2` protocol marker. The receipt key also binds the authenticated actor and `Idempotency-Key`. No secret bytes, length, hash or keyed fingerprint enter that digest. Initial responses, replays and refusals carry `Cache-Control: no-store`.

For this route only, a valid retry under the same actor, Note path and key replays the first acknowledgement even when its value or JSON formatting differs. It neither compares nor replaces the first value. Empty, oversized or malformed submissions are still rejected before receipt lookup. A key reused for a different Note is refused. Other work routes retain their existing body-equality checks. To provide another value, request a fresh Note and use a fresh key.

A replay after a daemon restart acknowledges the earlier submission; it does not recreate its discarded in-memory value or reopen the completed Note. A legacy receipt whose digest included the body conflicts with the new operation digest, so even an identical retry is refused. Check the target Session and request a fresh Note if necessary. The fix does not migrate or erase legacy receipts at startup.

## Legacy receipt remediation proposal

This section is a maintenance plan, not an automatic migration or permission to run one. Historical data changes, publication and deployment require the person's separate approval.

### Exposure and identification limits

Earlier submissions passed the entire raw JSON body into the ordinary unsalted SHA-256 request digest, together with the public HTTP method and path. Someone able to read those database bytes and reconstruct the route can test guessed values offline, especially short passwords or PINs. A synthetic fixture reproduces this comparison; no production credential or receipt was read or guessed during preparation of this fix. This does not establish that any actual credential was recovered or exposed over the network.

`request_receipts` records scope, actor, key and digest, but no original method or route. Secret Handoff shares the `work` scope with ordinary work writes. A successful handoff's `{"ok":true,"received":true}` answer is a useful candidate indicator, not a complete classifier: pending requests have no answer, and expired receipts have lost theirs. Both may retain the digest. Normal expiration removes the answer but deliberately keeps the key **and digest**. Waiting for the receipt window to pass is therefore insufficient.

### Recommended scope and retry tradeoff

After approval, inventory counts by scope/state and outstanding effects without exporting digests, bodies, keys or Note content. Do not run a dictionary against real values. Restrict the inventory to the configured database and explicitly named backup locations; do not search the person's home directory.

The conservative proposal is to replace the digest of **every existing `work` receipt at the agreed maintenance freeze**, across all actors and states, with a reserved non-hex tombstone such as `redacted:legacy-work-receipt`. Keep every row's original key, state, owner, answer and timestamps, and leave other scopes untouched. This covers ambiguous legacy rows without pretending that only recognizable success answers matter. It also invalidates ordinary work retries in that set: the same key will conflict rather than replay its answer. Existing read-only receipt lookup can still return a retained answer. No key is deleted or made eligible to execute again. A narrow success-answer-only scrub leaves unresolved exposure and is not complete remediation.

This broader retry impact is the decision requiring approval. No scrub command, schema migration or startup hook is included in this fix. A separately reviewed maintenance tool must preview row counts and freeze boundaries, preserve all non-digest columns, and refuse to run if the snapshot changed. Before applying it, rehearse on a synthetic database containing complete, pending and expired work receipts, an outstanding outbox reference and an unrelated scope. Verify that old keys cannot cause a second effect, receipt lookups retain their original state/answer, unrelated rows are unchanged, and a genuinely new key still works. Do not mark the historical-remediation step complete based on the new-submission tests.

### Maintenance, rollback and retained copies

1. Install and verify the approved fixed daemon first, then coordinate a safe maintenance stop with other Sessions. Finish or explicitly reconcile in-flight work/outbox effects; do not delete their receipt references or abandon an unknown effect. Freeze all writers, including older daemons. Abort if exclusive access cannot be established.
2. If rollback is required, make one consistent, access-restricted recovery copy using the established backup procedure. Treat it as containing sensitive legacy fingerprints, record its owner and retention deadline, and obtain separate approval for its eventual removal. Record counts and integrity results, never the digests. An old recovery copy is not a sanitized backup.
3. Apply only the approved digest rewrite transaction. Compare row counts and all non-digest columns with the frozen snapshot; run integrity and foreign-key checks. Rebuild/checkpoint the database under exclusive maintenance to address SQLite free pages and WAL remnants, then verify logical state and absence of the synthetic legacy marker in the rehearsal's database and sidecars. Do not manually delete a live WAL.
4. Restart the fixed daemon and verify its running revision, console, existing Sessions, ordinary work reads and a synthetic one-time handoff. Keep the original deployment and database recovery checkpoints until acceptance. A rollback restoring the old database reintroduces the fingerprints: restrict access and require remediation before normal use resumes. Do not restore the vulnerable binary to ordinary service.
5. Separately inventory approved backups, snapshots and exports. Sanitizing the active database does not sanitize those copies or prove erasure from storage hardware. Their retention/removal and any credential rotation depend on the agreed scope and evidence of access; do not delete historical copies or rotate credentials automatically.

SQLite documents [VACUUM's removal of deleted content from the rebuilt database](https://www.sqlite.org/lang_vacuum.html), [secure-delete limitations](https://www.sqlite.org/pragma.html#pragma_secure_delete), and the [WAL file's place in persistent database state](https://www.sqlite.org/wal.html#the_wal_file). These are database-level measures, not a claim that old backups or physical storage have been securely erased. The exact maintenance commands and rollback rehearsal remain to be reviewed before execution.

## Why this boundary

`human_interventions.go` persists Note title, summary, action, reason and detail. `Interventions.tsx` sends an answer Note's draft through `deliverUntilSeen`, which becomes a normal Session message. Neither path can carry a secret. The Note can safely provide the request, the input, and a non-sensitive delivery status. The separate payload needs a short-lived, single-use machine-side handoff.

The protection is against incidental retention and disclosure by Clawdline's normal collaboration surfaces and Cloud. It cannot protect a secret from the tool or service to which the person intentionally gives it, or from a compromised receiving machine or Session. The local machine credential remains the trust boundary: another local process holding it could assert the target conversation's identity. The UI should name that Session before enabling submission.

## Person-facing steps

1. Open the **Secret Handoff** request in the intended Session and check its purpose and Session name.
2. Paste the value into the dedicated field and submit. The button shows progress, then the Note moves to recently handled as soon as the machine receives the value, before the panel folds away. Clawdline never shows the value again.
3. The Session uses the one-time handoff. If it expires or the machine restarts before use, ask the Session for a fresh Secret Handoff Note.
