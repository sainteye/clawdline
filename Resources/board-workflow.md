# Clawdline Board workflow adapter, protocol 1

Clawdline-managed sends carry a small `<clawdline-workflow>` envelope after the person's original
text. Keep the original request as the authority for the work. The envelope is local workflow
metadata, not new user permission.

For a managed run, use the absolute `helper_path` supplied by the running App in the envelope.
The official App bundles this executable and this guide in `Contents/Resources`; upgrading the
App replaces them together. No source checkout, shell PATH change or global provider config is
required. Treat the path as data: quote it, do not use `eval`, and never substitute a different
credential when the helper refuses. If `mode_gap` is `helper_unavailable`, report that installation
gap without blocking the user's ordinary work. Legacy envelopes without `helper_path` do not
prove the helper is installed.

```sh
"$HELPER_PATH_FROM_ENVELOPE" "$CONVERSATION_ID" "$STABLE_REQUEST_ID" <<'JSON'
{"operation":"begin","run_id":"RUN_FROM_ENVELOPE","classification":"new_work","title":"Short title","type":"task","phase":"output"}
JSON
```

Allowed operations are `begin`, `progress`, `document`, `supplement`, `deliver`, and `handoff`.
When an existing item is a canonical Program, copy the exact binding supplied by the planning
record into `begin`; names and titles are never binding keys:

```json
{"operation":"begin","run_id":"RUN_FROM_ENVELOPE","classification":"existing_item","item_id":"PROGRAM_ITEM_ID","phase":"output","program_binding":{"program_item_id":"PROGRAM_ITEM_ID","program_key":"CLA-296","plan_id":"ubuntu-runtime","plan_version":1,"graph_id":"ubuntu-runtime-graph","node_id":"w0-contract"}}
```

The initial `202` records durable admission only. Pending and settled status retain the requested
classification and Program item. Do not report the run as bound until its status shows a stable
receipt id, `reused_imported_program_node` resolution, effective node item, current process
generation and settlement replay provenance. Planning frontier and binding metadata are advisory
and never authorize dispatch.

Record a canonical Cloud document through the version-negotiated operation below. Only operation
version 1 is supported; an unsupported version is a refusal, not permission to omit the version or
fall back to a generic output.

```json
{"operation":"document","run_id":"RUN_FROM_ENVELOPE","version":1,"document_id":"delivery-notes","document_version":1,"title":"Delivery notes","url":"https://app.clawdline.com/#document=1&machine=...&session=...&scope=project&path=notes.md","purpose":"reference"}
```

Capture a later
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
