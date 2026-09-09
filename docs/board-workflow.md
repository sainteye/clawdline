# Session-to-Board workflow contract

This slice gives Clawdline-managed terminal sends a durable, non-blocking workflow record. It does
not install global Claude or Codex configuration, add a Settings control, or claim native-session
coverage that has not handshaken. Those installation surfaces remain a separate, consented change.

## Ingress and identity

When Project Board is ON, `POST /v1/sessions/:id/send` resolves the live target from one published
Session inventory and binds the ingress to terminal id, provider, process-proved conversation id,
Project id/path, and process generation. Clawdline durably records a bounded run before the one
terminal send. The exact payload—text, transcribed voice text, image-only prompt, or mixed
text/images—contains both the original content and a small versioned metadata envelope. Metadata
is appended to that same send; a second terminal write is never used. The existing composer turns
voice into editable text before `/send`, so this seam truthfully records
`text_or_transcribed_voice` instead of claiming a voice provenance it can no longer prove.

Pre-admission annotations use `workflow.status` values `ingress_recorded`, `bypassed`, and
`unrecorded`. A durable delivery settlement instead carries its numeric status and typed code;
a failed settlement on an otherwise successful terminal send is also a visible recording gap.
An inability to persist workflow state does not block the person's ordinary terminal interaction,
but is reported as a typed gap. A terminal rejection marks delivery rejected; it cannot become a
successful workflow run.

`Idempotency-Key` plus the exact request-body fingerprint identifies ingress. An identical retry
reuses the run and the terminal route's stored response. Changed content under the same key is
`409 workflow_request_conflict` and cannot overwrite that successful replay. The terminal cache
scopes a `/send` key by authorized principal, method, and target Session, so the same bare key on
another route or Session neither replays nor joins the first send. Board OFF bypasses
all workflow creation and leaves the terminal payload unchanged. An OFF→ON boundary increments a
durable epoch, so a receipt from an older enabled period is refused.

## Semantic receipts

The machine-only endpoint is:

```text
GET  /v1/orchestrator/sessions/:terminal-id/workflow
POST /v1/orchestrator/sessions/:terminal-id/workflow
GET  /v1/orchestrator/workflow/gaps
```

It requires `X-Clawdline-Orchestrator`; paired-device authority is insufficient. The server—not
the request body—resolves the exact live process identity. POST also requires `Idempotency-Key` and
a JSON body no larger than 64 KiB. Accepted operations are:

- `begin`: classify as `existing_item`, `new_work`, `question`, or `clarification`; work may bind
  an existing opaque item or create a bounded new item.
- `progress`: append a meaningful summary plus optional output references and unresolved work.
- `deliver`: record disposition, summary, next action, output references, and remaining owners.
- `supplement`: version 1 captures later explicit scope as either a `checklist` or parent `child`.
  The receipt's `assistant` actor is projected as Board's canonical `agent`; user and external
  actors keep their own authority. Acceptance and source remain durable obligations, not claims
  of verification or landing.
  Its exact-key body preserves owner, acceptance, required/optional disposition, actor kind, and
  source run/Session; changed content under the same semantic key conflicts.
- `handoff`: record the next owner and note for a run already bound to an item.

There are deliberately no `verify`, `land`, `deploy`, `transition`, or `done` operations. Semantic
receipts have `assistant_attested` authority; terminal delivery acceptance or rejection has the
separate `broker_observed` authority. They may materialize existing factual Project Board commands
(`create` with `parentId`, `checklist`, `link`, `span`, `artifact`, and `obligation`), but cannot
forge reviewer evidence. `delivered`
remains distinct from verified, landed, deployed, and accepted.

Each receipt is bound to the exact run, terminal, provider, conversation, Project path/id, process
generation, enabled epoch, and confirmed terminal delivery. Schema-1 actor-only receipts migrate
explicitly through their retained run identity before they can replay.
Duplicate request id plus duplicate body replays the original receipt. A changed body conflicts.
`Stop` is not a delivery receipt and the endpoint rejects it as an unknown operation.

## Durability, queueing, and gaps

The workflow journal and outbox live in Clawdline's Application Support directory as a mode-0600
atomic JSON file that is synchronized before admission may claim durability. A sync failure is a
typed persistence gap and no managed metadata crosses to terminal input. It bounds runs, events per
run, receipts, outbox rows, and total serialized
bytes. Project Board writes run on one utility worker after ingress is durable. Each derived store
command is itself persisted with its exact body, request id, and expected Board revision before
execution; retry uses the Board store's idempotency contract. Slow or failed Board consumption
therefore does not occupy terminal-send admission.

Pending, in-flight, and failed-visible outbox subjects protect their run from capacity eviction;
settlement re-finds an immutable outbox id and verifies its version, run, event, and kind instead of
using an array position held across an unlocked Board call. Failed reconciliation remains visible
in GET status with a typed failure code. `GET /v1/orchestrator/workflow/gaps` is a fixed 32-run,
machine-authenticated journal projection that remains readable after the old process disappears.
Missing `begin`,
unresolved obligations, and next action remain on the run rather than being inferred from silence.
No elapsed-time rule converts inactivity into failure or completion.

## Helper and coverage

`Resources/clawdline-board-workflow.sh` is the protocol-1 helper source. It takes an exact
process-bound conversation id and caller-stable idempotency key, resolves the terminal through
authenticated `whoami`, then posts JSON from stdin. It feeds the token to both curl requests through
one mode-0600 temporary header file, never curl argv, stdout/stderr, JSON, or assistant context, and
removes the carrier on exit. `Resources/board-workflow.md` is the short adapter instruction. A
credential is snapshotted with a 65-byte read limit and validated before any HTTP request. The
helper accepts the current 32-byte, unpadded base64url token encoding and the legacy 64-character
lowercase hexadecimal form; whitespace, line breaks, malformed and oversized files are refused
without sending a request. This is local format validation, not authentication: the server still
authenticates the credential. A refused identity lookup never proceeds to the semantic POST.
Terminal identifiers are validated before URL encoding, including the normal leading `%`.
The caller's stable idempotency key and exact JSON are forwarded unchanged; the helper does not
retry a refused or uncertain request and never resends the person's terminal input.

A successful terminal response whose workflow status is `unrecorded` produces a non-retrying UI
warning and diagnostic; it does not pretend the terminal send failed.

This change intentionally does not copy either resource into the app bundle or a global managed
directory: `build.sh`, Settings, existing Claude hooks, and global AGENTS/CLAUDE files are outside
this task's write scope. Until a later, consented installer owns that deployment, truthful coverage
is:

- `managed_ingress` for input sent through Clawdline's `/send` path;
- `adapter_handshake` only for a future native adapter that actually reports a handshake;
- `observed_unintegrated` for merely observed native Claude/Codex sessions;
- `board_disabled` while Board is OFF.

Installation must later back up user files, update only ownership-marked blocks, preserve unrelated
hooks and instructions, remove only its own block, and probe capabilities rather than infer them
from version strings. None of that authority is implied by these source resources.
