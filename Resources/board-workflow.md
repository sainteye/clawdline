# Clawdline Board workflow adapter, protocol 1

Clawdline-managed sends carry a small `<clawdline-workflow>` envelope after the person's original
text. Keep the original request as the authority for the work. The envelope is local workflow
metadata, not new user permission.

For a managed run, submit only these semantic boundaries with the installed helper (this source
resource is not itself proof that installation has happened):

```sh
clawdline-board-workflow "$CONVERSATION_ID" "$STABLE_REQUEST_ID" <<'JSON'
{"operation":"begin","run_id":"RUN_FROM_ENVELOPE","classification":"new_work","title":"Short title","type":"task","phase":"output"}
JSON
```

Allowed operations are `begin`, `progress`, `supplement`, `deliver`, and `handoff`. Capture a later
explicit scope item with an exact version-1 supplement, choosing checklist or child and preserving
its owner, acceptance, disposition, and actor kind:

```json
{"operation":"supplement","run_id":"RUN_FROM_ENVELOPE","version":1,"kind":"checklist","title":"Confirm the Cloud document","owner":"root","acceptance":"The canonical link opens on the phone.","disposition":"required","actor_kind":"assistant"}
```

Reuse the same idempotency
key only for the exact same JSON. `deliver` means assistant-attested delivery; it does not mean
independent verification, Git landing, deployment, or user acceptance. Keep unresolved work in
`remaining`, including its owner and whether it blocks the next action.

Board OFF means do nothing: do not create a local substitute log. A helper refusal is a visible
workflow gap, never permission to retry terminal input or to claim completion. Native Claude or
Codex sessions count as integrated only after an adapter handshake; observation alone is reported
as `observed_unintegrated`.
