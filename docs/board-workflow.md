# Session-to-Board workflow contract

## Discoverable installed contract and delivery projection

The installed helper supports `--help` as a local, credential-free read of the bundled
`board-workflow.md`. The English and Traditional Chinese agent guides point to it, and
`clawdline-skill.sh list` / `get board-workflow` expose the same bytes after relocation or upgrade.
Do not POST the managed envelope as a command or guess missing fields. A refused `begin` remains
a visible workflow gap; a separate Session `/complete` receipt does not repair it.

After an exact managed `deliver` ends its own declared interval, the durable outbox emits
`record_session_delivery` with the same run, item, provider, Session, start request and event.
This is **assistant-attested delivery only**: it supplies no verification, landing, release or
lifecycle authority. `waiting_*`, interrupted and cancelled dispositions remain follow-up states;
a planning delivery remains planning, not completed implementation.

Retained legacy runs may backfill only the missing projection when their exact span and every
old intent are settled. Existing command bodies and settled identities are preserved. Each pass
is bounded by eight additions, outbox capacity and the encoded journal byte budget. Optional
backfill that cannot fit is deferred without discarding loaded state or changing the journal.
A mandatory migration write failure makes storage unavailable, never a healthy empty replacement.
Missing identities remain historical gaps; no guessing or old-command replay is permitted.

The Board records the observed current scope only when the source delivery is not older than
the latest scope change. Otherwise scope is unresolved, not reconstructed by subtracting one.
Any later identifiable declared interval (including ended handoffs) makes an earlier delivery
historical. Equal start timestamps are conservatively unresolved rather than evidence of order.

This slice gives Clawdline-managed terminal sends a durable, non-blocking workflow record. It does
not install global Claude or Codex configuration, add a Settings control, or claim native-session
coverage that has not handshaken. The official App packages the protocol-1 helper and guide;
managed Claude and Codex sends receive the same absolute, executable bundle `helper_path`.
This optional v1 field is presentation-only data, not a shell expression or authorization. The
path is resolved once at bootstrap from the App resource directory, not PATH or the source repo.
Missing, non-executable, directory and symlink candidates produce `helper_unavailable` without
blocking ordinary sends. No provider config or shell profile is rewritten. Legacy envelopes still
render; native sessions without an adapter handshake remain `observed_unintegrated`.

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

## Begin assistance in the envelope

Version-1 envelopes may carry two optional fields, following the `helper_path` precedent: optional
in v1, strictly validated, and absent when not applicable. Both are advisory data for the
assistant. Neither is a command, a binding or an authorization, and neither changes the run, its
classification, the helper or server validation. `begin` still requires an explicit
`classification` and, for `existing_item`, an exact `item_id`; no turn skips it.

- `begin_template` is present on every run admitted by a producer that knows it. It is this run's
  ordinary `begin` with `run_id` filled in: `operation:"begin"`, a `classification` holding the
  choice list `existing_item|new_work|question|clarification`, the placeholders
  `item_id:"<existing_item>"`, `title:"<new_work>"` and
  `type:"<new_work:task|feature|bug|refactor|coordination|epic>"`, and `phase:"output"`. It is not
  postable as it stands: the server refuses the choice list as `workflow_begin_invalid`, and every
  real classification also requires deleting the fields that do not belong to it. With it, an
  ordinary `begin` needs no contract read. The contract (`--help` or `get board-workflow`, the same
  bytes) is read once, only for a Program binding, a document, a supplement, a handoff, an
  assignment decision, or after a typed refusal.
- `previous_item` appears only beside `begin_template`, and only when there is one. It is the exact
  item id of the latest earlier run with the same provider, conversation id and Project id in the
  current enabled epoch whose effective item is settled. `Run.itemID` is that settled effective
  item: a plain `existing_item` begin binds its exact id at admission, while `new_work` and a
  Program binding leave it nil until the Store receipt settles it, so a pending create or a pending
  or failed Program binding is never offered. An item whose Board `link` was refused (for example
  `item_not_found`), or whose id is not a lowercase UUID (a human key such as `CLA-395` passed as an
  id), is unresolved and skipped. Only the id is sent: the journal holds no label for a plain
  `existing_item` begin, a `new_work` title may be 300 bytes, and titles are never keys. It is never
  drawn from another conversation, provider, Project or epoch.

The derivation reads only the in-memory journal inside `prepareIngress`, under its existing lock
and before capacity eviction. There is no Board Store read, no file or network I/O, no new lock and
no model call. **The hint is frozen on the run at admission** (`Run.beginHint`, an optional journal
field with no schema-version change and no migration), because an identical retry can reach the
terminal again. A `429 busy` or maintenance refusal is not stored in the ten-minute `/send` replay
cache, and that cache lives only in memory, so a retry after such a refusal, after the cache
expires, or after an App restart re-enters `prepareIngress`. That call rebuilds `wireText` for the
existing run, and the rebuilt text is typed. Rebuilding from the frozen hint keeps it byte-identical
even if another run settled or was evicted in between. A run admitted before the field existed has
no `beginHint` and replays the legacy envelope it was first sent with. Together the two fields add
a constant 337 bytes to a hinted envelope.

**Byte identity lasts only as long as the retried run is retained.** The journal holds at most 256
runs. At that capacity a new admission evicts the oldest safe candidate: either a fully followed-up
run, or a delivered/rejected run whose missing follow-up has been abandoned for 24 hours. Only a
`pending`/`inflight` outbox row protects a run; a terminal `failed` row remains visible as retired
gap accounting but cannot pin the hot journal forever. The retired summary durably counts runs
missing `begin`, runs missing `deliver`, terminal-failed outbox runs and rows, and provider totals.
Terminal failure codes are counted as well, so retirement does not erase the reason.
It is unproved history, not a claim that the omitted work was repaired. The evicted run's payload,
outbox rows and receipts leave together. Once the retried run has
been evicted, a later identical retry finds no run. It is a new admission: it derives the hint again
from the journal as it is at that moment, under the same deterministic run id, so its
`previous_item` can differ from the original send. When the original was delivered, that retry
types the prompt into the terminal a second time. That is the pre-existing `/send` idempotency gap,
not something this field introduced: the replay cache lives only in memory for ten minutes, so once
it forgets, even a retained run's retry is typed again. Root owns that gap as a separate line of
work. No tombstone or other durable state is kept for an evicted run.

**A downgrade from schema 3 drops the frozen hint.** `beginHint` was an optional field on schema-3
runs, not a schema bump. An older App that reads schema 3 loads a journal containing it, ignores the field, and
drops it the next time it persists, because every persist re-encodes the whole journal from that
App's own types. After re-upgrading, those runs have no `beginHint`, so a retry of one replays the
legacy envelope, which both decoders still accept. A schema bump was the alternative and was
rejected: the older App would refuse the whole journal as `workflow_store_invalid`, and every
managed send would then go out unrecorded (`workflow_persistence_failed`) until the upgrade came
back. Failing closed would stop Board workflow recording on a downgrade to protect an advisory
field. Schema 4 deliberately chooses that stronger rollback boundary for retirement evidence:
schema-3 binaries refuse a schema-4 journal rather than silently dropping `retiredGaps`, per-run
terminal failure totals, or exact failed-intent identities during their next whole-file rewrite.

Compatibility: the native (`Transcript.swift`) and web (`board-workflow-record.js`) decoders accept a
legacy envelope without either key and a new envelope with valid values. They still reject unknown
top-level keys, and a `previous_item` that is not a lowercase UUID or that appears without the
template. `begin_template` is held to a structural v1 contract, identical in both decoders, rather
than to the producer's current text: it is a JSON object with 2 to 16 keys, each spelled
`[a-z_]{1,64}`; every value is a string of at most 256 UTF-8 bytes; `operation` is exactly `begin`;
and `run_id` equals the envelope's own `run_id`. Nothing else is required, so a later producer can
reword a placeholder, change the choice list, or add or drop an optional field, and the envelopes
already in transcripts keep folding. A rejected envelope stays visible prose.
`Tests/board-workflow-metadata-v1.json` is one exact producer line: the Swift producer test must
emit it and the web decoder test must accept it.

Rollout: publish the hosted console at app.clawdline.com, which renders with the same web bundle,
completely first, and release the Mac App that starts emitting these fields only after that. A
console page loaded after the publish has the new decoder, and it also folds the legacy envelopes an
older Mac App still sends. The order does not protect a page that is already open. A standalone PWA
keeps the decoder it loaded and never reloads itself (`Resources/web/app/js/net/build.js`); once
the Mac build changes it shows the stale-build notice, and until the person reloads it, every new
envelope appears as raw prose. The structural template contract does not help such a page: it
governs only decoders that ship with it, and a decoder built before these fields existed rejects
both of them as unknown keys.

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
  New checklist supplements also persist one system-generated `supplementRelationId` per event.
  Their checklist and obligation projections share that exact ID; neither the title nor the
  run/Session pair alone identifies one supplement. Old events without an ID retain their exact
  pending replay bodies and are not retroactively matched by text.
- `handoff`: record the next owner and note for a run already bound to an item.
- `assignment_decision`: for a run already bound to the assigned item, submit
  `assignment_id`, `decision` (`accepted` or `declined`) and `note`. The receiver's
  provider/conversation/Project come only from the process-bound run, never JSON.
  Admission means journaled, not accepted responsibility: inspect the outbox and
  Board's matching `sessionAssignment` receipt before declaring takeover complete.

### Explicit Session responsibility

A writable Board client may propose `assign_session` with the exact item, Project,
provider and conversation UUID plus a note. The selected Mac's Board Store owns the
proposal; a terminal address or title cannot identify its receiver. This records
an intention, not proof that the Session exists, is online, idle or available.
It does not send, resume, spawn or interrupt a Session. The current owner remains
responsible until an explicit receiver decision is applied.

The proposal captures current owner and scope. The receiver begins a managed run
bound to that item, then explicitly calls `assignment_decision`; `begin` alone is
not acceptance. Only the matching process-bound provider/conversation and Project
can decide the exact proposal. Acceptance refuses a changed owner/scope and closed
work; decline may release a stale proposal without taking ownership. A writable
Board client may withdraw an exact pending proposal with `cancel_session_assignment`.
After withdrawal or replacement, an old decision cannot settle another proposal.

The legacy generic `accept_handoff` cannot accept a typed Session assignment.
Receipts remain `pending`, `accepted`, `declined` or `cancelled`; accepted transfers
owner and ensures a Session relation atomically. The last settled receipt and
history survive restart. Earlier settlements remain in bounded history. The same
request replays its receipt; uncertain workflow delivery retains the exact outbox
body rather than accepting twice. No assignment command promotes execution,
verification, landing or deployment. Existing v1 workflows without the optional
decision operation retain their prior behavior.

### Program-bound runs and versioned documents

Review, test, confirmation and correction are execution details of an existing human objective;
they use an exact existing item or broker child identity rather than `new_work`. Historical
mistakes are corrected only by the machine-only, UUID-pinned `reconcile_catalog` audit operation.
That operation changes catalog placement, not lifecycle or evidence authority, and is never a
title-based merge or a substitute for a managed run's missing follow-up.

An `existing_item` begin may carry `program_binding` with exactly
`program_item_id`, `program_key`, `plan_id`, `plan_version`, `graph_id`, and `node_id`.
The selected `item_id` must be the same canonical Program. The durable outbox first sends an
in-process-only `program_binding` Store command containing those identities plus the journal's
run, Session, provider, Project, and process generation. Until the Store returns its exact durable
receipt, the run has no effective item and reports `binding.status:"pending"`; the semantic POST's
`202` means only that this request and outbox intent were fsynced. Settlement records the exact
requested classification/item, stable receipt id, `reused_imported_program_node` resolution,
effective child item, process generation and `settlement_replay` provenance, then derives that
node's canonical Session link and a distinct activity span. Store replay and workflow restart retain
the original receipt identity. A later run upserts the same source-aware Session relation but retains
its own span.

The Store refuses a stale Plan version or an unknown graph node. Retained historical binding
receipts survive a valid successor Plan, but they neither match nor authorize a different current
Plan/node request. It never resolves by Program, node, or Session title. The receipt and planning
frontier say `advisory_only`; neither can dispatch broker work or replace broker execution authority.

`document` has a separate closed schema: `version` (currently exactly 1), `document_id`,
`document_version`, `title`, canonical `app.clawdline.com` document `url`, `purpose`, and optional
`supersedes_id`. Unknown fields—including authority or process identity supplied by the caller—are
refused. The workflow journals it before returning `202`, and the outbox materializes the ordinary
Store `document_reference` on the effective item. Restart replays the exact original request.
Unsupported operation versions return `workflow_document_version_unsupported`; they are never
silently interpreted as the current format. This reference does not mutate scope, lifecycle,
evidence, landing, or acceptance.

### Session assignment picker

The item detail's **Assign Session / view proposal** entry opens a separate sheet, not a
permanent chat banner. It reads the selected Mac and Project before offering uniquely observed
Claude/Codex conversation identities in that exact Project path. Titles are labels, never keys.
Missing inventory, ambiguous identity, stale Board model, old runtime without `sessionAssignment`,
or disabled writes refuses creation. Selection and an explicit note precede **Confirm proposal**.

The sheet records a proposal only: **awaiting acceptance**. This picker does not send notifications;
it cannot say whether somebody else has notified the receiver. It offers a deliberate
open-receiver action, copyable takeover context, and exact-proposal withdrawal. Receiver navigation
requires a currently observed exact provider/conversation/Project/Mac and unique UI address. An
unavailable receiver is refused; this action never falls back to untyped history or resume. Opening or copying
does not send, resume or accept on behalf of the receiver. Automatic request delivery is not in this
slice; the receiver still uses the process-bound helper to accept or decline. The last durable
settlement is shown independently from the current pending proposal.

Before sending, the browser retains the exact target, request ID, revision and command in a bounded
local intent journal (32 entries / 64 KiB), with a separate immutable key per request. A settlement
removes only its own exact record, not a newer same-item intent or any unrelated scope. Concurrent
intents remain individually retryable; none are silently overwritten by an aggregate journal update.
Storage failure refuses before sending. Reload never
resends automatically; explicit retry reuses that exact body and machine. A deterministic refusal
clears the intent and requires a fresh read/user confirmation; uncertain delivery retains it.
Neither the journal nor the UI is authority for lifecycle, verification or landing.

There are deliberately no `verify`, `land`, `deploy`, `transition`, or `done` operations. Semantic
receipts have `assistant_attested` authority; terminal delivery acceptance or rejection has the
separate `broker_observed` authority. They may materialize existing factual Project Board commands
(`create` with `parentId`, `checklist`, `link`, `span`, `record_output`, and `obligation`), but cannot
forge reviewer evidence. `delivered`
remains distinct from verified, landed, deployed, and accepted.

Each receipt is bound to the exact run, terminal, provider, conversation, Project path/id, process
generation, enabled epoch, and confirmed terminal delivery. Schema-1 actor-only receipts migrate
explicitly through their retained run identity before they can replay.
Duplicate request id plus duplicate body replays the original receipt. A changed body conflicts.
`Stop` is not a delivery receipt and the endpoint rejects it as an unknown operation.

An item-bound `deliver` (including waiting/interrupted/cancelled dispositions) or `handoff`
also appends a durable `end_span` intent. It identifies the successful begin's exact Board
request, actor, item and Session; it never closes whichever newer interval happens to be active.
The end does not change scope, verify, land, accept a handoff or complete a Feature. Replays,
restart and an uncertain Board response retain the same outbox identity. A missing/failed begin
or a legacy span without an exact declaration identity stays a visible reconciliation gap,
not a guessed end or a successful empty update. Historical gaps need source-specific repair;
elapsed time and a dead Session alone are not completion evidence.

New workflow output references use the in-process-only `record_output` operation. These references
retain URLs and titles but neither invalidate accepted scope nor reconcile lifecycle. They are
marked `referenceOnly` and cannot be accepted as an artifact to manufacture verification. Public
`artifact` commands still represent changed output scope and invalidate its proof. Old journal
intents of kind `artifact` keep their exact replay body; an upgrade never rewrites an uncertain
request into a different operation. Previously invalidated proof is not automatically restored.

Legacy unended declarations without the actor/request-derived span identity stay open in history,
with `legacy_span_identity_unresolved` and an unresolved-span count. They do not establish current
activity or inflate Project active counts. Identifiable declarations and broker activity retain
their existing semantics; neither elapsed time nor a missing Session closes an interval.
Source-specific historical proof repair remains separate work.

## Historical criterion applicability repair

`node tools/repair-board-criterion-proof.mjs manifest.json` previews an operator-authored repair.
The version-1 manifest has exactly `version`, `itemId`, `projectId`, `scopeRevision`, `evidenceId`,
`subject`, and `criteria:[{checklistId,reason}]` (1–32 unique rows). The operator must first inspect
the retained test/review receipt and explicitly justify why its existing verification applies to
each named criterion. The tool does not infer applicability from a title or a generic summary.
Reason boundary whitespace is normalized before hashing and sending, so Store trimming cannot
make a committed attestation unrecognizable on resume.
It requires a complete ready snapshot, the exact same item/Project/current scope and subject,
one retained passed verification, and already-passed criteria without other attached proof.

After reading the preview, apply it with the returned digest and a private retry-state path:

```sh
node tools/repair-board-criterion-proof.mjs manifest.json --apply <digest> --state /private/tmp/criterion-repair-state.json
```

The existing machine-only `record_evidence` operation records an explicit **root applicability
attestation**, citing the retained proof; this is not another test execution or broker proof.
Each criterion has a deterministic source identity. It never sends a transition, landing, review
resolution or span command. Ordinary Store reconciliation still applies to the newly linked
criterion evidence; no new lifecycle or verification authority is introduced.

Every write rechecks identity/scope and uses revision CAS. The exact request is fsynced to the
private state before sending, and an uncertain response retains it for an identical retry.
Only an observed matching proof is reported complete. A definite refusal stops the batch; changed
scope, missing/truncated proof, an already-different link or a tampered retry file fails closed.
This is a resumable sequence, not an atomic batch: earlier confirmed rows can remain applied.
Keep the same manifest and state path when resuming; a busy/orphaned lock requires an operator
to establish that no repair process remains before removing that lock. Do not run concurrent
repairs of the same item. Neither elapsed time nor this tool repairs unidentified legacy spans,
revives stale verification scope, or converts a review into a landing.

Networking is limited to the local machine's loopback Board API (default port 7717,
`CLAWDLINE_PORT` override); redirects are refused. The machine credential is read inside the
process from the existing private token file, never from argv, manifest, checkpoint or output.
Preview requires the same read credential but performs no command or checkpoint write.

## Root Session landing projection

The broker's Git-verified Session landing has a separate, in-process producer. It is not a new
helper operation and cannot be minted through public Board commands, even with machine authority.
After releasing its registry lock, the broker journals the projection before replying; the reply's
`boardProjection` gives the exact status, code, run/item identity and outbox count. The Board command
itself runs later on the bounded workflow worker. A projection refusal does not undo a proved Git
landing and must not be reported as successful Board reconciliation.

Binding requires the same terminal, provider, conversation, process generation and canonical Git
common directory as the latest managed ingress at the landing time. That ingress must have a
delivered, item-bound work receipt in the current enabled epoch. A newer question or unbound run
never falls back to an older item. Missing bindings produce `root_landing_binding_unresolved`;
disabled mode, capacity and persistence failures keep their typed refusals. Retained broker receipts
are retried at startup and on the bounded catalog timer. Once journaled, the exact outbox survives
broker receipt consumption and retries with the same prepared request after uncertain replies.
Unfinished outbox rows pin their source events; semantic recording returns
`workflow_event_capacity` rather than evicting a still-needed event. Unmanaged roots are reported
as `root_landing_unmanaged` and do not mark unrelated Projects incomplete. Managed failures have
Project-scoped current coverage, bounded to 128 retained root sources. Consumed or capacity-evicted
source gaps are counted separately as unproved history, not reported as repaired; this coverage
is process-local, while an admitted root event and its outbox are durable.

The Store records broker evidence without fabricating a Task. Only an exact canonical commit SHA
matching the current verification subject and scope can supply its current landing pointer.
Delayed evidence remains history; landing never implies deployment or resolves unrelated obligations.
Old consumed broker receipts and missing historical workflow identities are not reconstructed.

## Durability, queueing, and gaps

The workflow journal and outbox live in Clawdline's Application Support directory as a mode-0600
atomic JSON file that is synchronized before admission may claim durability. A sync failure is a
typed persistence gap and no managed metadata crosses to terminal input. It bounds runs, events per
run, receipts, outbox rows, and total serialized
bytes. Project Board writes run on one utility worker after ingress is durable. Each derived store
command is itself persisted with its exact body, request id, and expected Board revision before
execution; retry uses the Board store's idempotency contract. Slow or failed Board consumption
therefore does not occupy terminal-send admission.

Completed intents are atomically compacted into identity-bound tombstones on their source event.
The tombstone retains the kind, index and created-child identity needed by downstream commands;
materialization consults it before creating another outbox row. It grants no verification or
landing authority. Tombstones leave only when their bounded source event/run is safely evicted.
The exact successful span request ID is additionally retained on its immutable run binding, so
ending an interval still works after begin-event retention and restart; no span identity is guessed.
Legacy completed rows whose source event was already evicted can leave without rematerialization,
but their exact span-start identity is preserved. Conflicting live rows and settled IDs are refused.
Admission reserves deferred create/binding/child fanout before any Board mutation executes, so
settlement cannot silently exceed live outbox capacity. A terminal failed row becomes bounded
failure evidence and leaves the executable queue atomically; `outbox_pending: 0` is still not a
claim that terminal failure history is absent.

Journal schema 4 reads legacy schema 1/2/3 within the existing byte limit, validates source
identity, and compacts proven completed and terminal-failed rows before testing live capacity.
Terminal failures retain a bounded per-run code/count summary and exact identity beside a retained
source event, but their raw queue rows leave atomically and cannot reserve executable capacity.
This includes legacy 514-row
journals created by the former post-create fanout overflow. Migration must be durably synchronized
before it is installed in memory. Unknown/conflicting settlement identity, unresolved over-capacity
work, and persistence errors remain explicit refusals; no journal clearing or blind resend is used.
Legacy `artifact` intents and current `record_output` intents share the exact
`<event-id>-artifact-<index>` identity. Migration accepts only these two declared spellings,
retaining the original kind and nonnegative output index; it does not infer aliases from titles.
Duplicate IDs, mismatched indexes, live/settled collisions and unknown kinds still refuse.
The focused migration fixture can additionally consume a read-only journal copy through
`CLAWDLINE_LEGACY_WORKFLOW_FIXTURE`; it copies bytes into its private test directory and uses a
stub Board sink. A passing copy test proves migration and the semantic begin boundary, not a
successful helper call against the installed runtime; that still requires post-rollout acceptance.

Pending and in-flight outbox subjects protect their run from capacity eviction;
settlement re-finds an immutable outbox id and verifies its version, run, event, and kind instead of
using an array position held across an unlocked Board call. Failed reconciliation remains visible
in GET status with a typed failure code. `GET /v1/orchestrator/workflow/gaps` is a fixed 32-run,
machine-authenticated journal projection that remains readable after the old process disappears.
Its `retired` block durably counts expired missing-begin/missing-deliver runs, terminal-failed
outbox runs and rows, failure codes, and accountable providers after those payloads leave the hot
journal. Missing `begin`, unresolved obligations, and next action remain on the live run rather
than being inferred from silence. No elapsed-time rule converts inactivity into failure or
completion.

A journal run exists only after the producer deliberately admitted managed ingress. Every such
envelope requires `begin`, including `question` and `clarification`; a delivered run that still
misses `begin` is therefore an agent protocol omission, not evidence that the turn should have
been unmanaged. Expiry keeps that omission measurable while preventing it from disabling all
later sends on the machine.

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
Both loopback hops have a two-second connect timeout and a fifteen-second whole-transfer timeout,
so a wedged local listener cannot retain the calling terminal indefinitely.

A successful terminal response whose workflow status is `unrecorded` produces a non-retrying UI
warning and diagnostic; it does not pretend the terminal send failed.

The official build packages the helper and guide in the App bundle. It does not install global
hooks or edit user/provider configuration. Truthful coverage is:

- `managed_ingress` for input sent through Clawdline's `/send` path;
- `adapter_handshake` only for a future native adapter that actually reports a handshake;
- `observed_unintegrated` for merely observed native Claude/Codex sessions;
- `board_disabled` while Board is OFF.

Installation must later back up user files, update only ownership-marked blocks, preserve unrelated
hooks and instructions, remove only its own block, and probe capabilities rather than infer them
from version strings. None of that authority is implied by these source resources.
