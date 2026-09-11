# Project Board implementation contract

Project Board is **enabled by default and currently free**. This page describes the implementation boundaries and wire contract; [Project Board](project-board.md) explains the user workflow. Existing safeguards remain in standard mode.

## Component ownership

### Assistant-attested Session delivery

Internal `record_session_delivery` accepts only workflow-origin calls whose exact actor
`workflow:<provider>:<conversation UUID>`, item, project, start-request identity and phase match
an ended non-broker declared interval. Public callers cannot create this fact. Event identity is
immutable: exact replay is a no-op, changed facts conflict, and bounded capacity never evicts
old evidence to admit a new delivery.

`progress.sessionDelivery` exposes the latest retained event with `authority:assistant_attested`
and a separate `current` flag. `scopeRevision` is either the observed revision or null;
`scopeStatus` is `observed` or `unresolved`. Unknown historical scope is never guessed.
Newer broker activity, scope changes or later identifiable declared spans invalidate currentness;
ending a newer span does not resurrect an older delivery. The fact survives Store reload but
does not call lifecycle reconciliation, change scope, mint verification or create landing/release
evidence. Trusted verification with missing required checklist evidence is presented as acceptance
incomplete, rather than silently falling back to planning or claiming completion.

### Session navigation and Info relationships

Board Session titles are clickable. A shareable address uses the canonical hosted origin and
`#session_ref=1&machine=...&conversation=...&project=...` (Project is optional). The closed,
bounded locator carries stable machine and provider conversation identities, never a title or a
reusable terminal ID. Cold links wait for inventory; a unique matching live Session opens without
sending anything. A missing live Session may use its exact Project's bounded provider history;
resume always requires an explicit click and retains its existing durable mutation fence.
Unavailable, ambiguous, unauthorized and incomplete-history results remain visible refusals.
Local `this-mac` aliases cannot become share URLs: their titles use local observed navigation
until a canonical machine identity is available.

Session Info contains the collapsed related-Board section; it is not a persistent chat banner.
Cloud discovers Board mode and relationships from the selected Session's explicit machine read,
independently of the boot-time settings request. Unknown mode, read failure and authoritative OFF
are distinct. Switching machine/conversation fences late results. Inventory renders do not poll;
an unavailable read offers explicit refresh and never gates transcript or message delivery.

- `ProjectBoardStore` owns persistence, mode, work items, lifecycle gates and command receipts.
- `ProjectBoardIntegration` projects trusted broker and UsageLedger facts without transferring their ownership to the board.
- `ProjectBoardNarrative` schedules bounded background reading-text generation through the existing
  naming runner; it owns no lifecycle or evidence authority.
- `ProjectBoardRequestCoordinator` owns bounded Board request execution after transport admission: one serial command writer and a separate serial read/encode lane.
- `ProjectBoardHTTP` owns transport authority and the final response byte budget; local and Cloud requests use the same service.
- `view/board.js` renders evidence-driven, read-only Project snapshots. `input/board-settings.js` owns navigation and the administrative mode command, not a second persisted mode.

## Store boundary (Foundation/CryptoKit only, independently testable)

`final class ProjectBoardStore` with `static let shared`, `init(url: URL)` for isolated tests. Default owned persistence under Application Support/Clawdline, env override `CLAWDLINE_BOARD_STORE` for tests. One serialized owner, atomic JSON durable writes, bounded inputs, reject corrupt/unknown-version store without replacing it. Persistence includes settings, items, immutable history and request receipts; Orchestrator and UsageLedger remain separate owners.

Adapter interface:

- `snapshot(project: String? = nil, item: String? = nil) -> [String: Any]` returns complete envelope `{"board": ...}`. Bounded scan/pagination with honest truncation allowed; expose it.
- `collectionSnapshot(project:item:kind:offset:) -> Reply` reads one 64-row continuation page for
  `document_references`, `remaining_work` or `user_decisions`. It is selected-item only, reports
  `totalCount` and `nextOffset`, and never materializes an unbounded all-Project response.
- `reportSnapshot(project:item:report:) -> Reply` retrieves exactly one opaque report version with
  strict Project/item ownership checks. It does not add superseded bodies to the catalog or normal
  item materialization and remains under the Store snapshot budget.
- `command(_ body: [String: Any], actor: String, trusted: Bool = false) -> Reply` where Reply has `status: Int`, `body: [String: Any]`. Errors body `{"error":{"code":...,"message":...}}`. Success full snapshot plus `itemId` when applicable.
- `ensureProject(id: String, name: String) -> AutomaticMutationOutcome` persists id/name only; the adapter resolves canonical identity. No paths in public record.
- `var enabled: Bool { get }` durable default true. Mode and entitlement owned here, not a second Config boolean.
- `ingest(task: [String: Any], projectID: String) -> AutomaticMutationOutcome` receives trusted broker records; idempotently links explicit `workItemId`, or a Project-scoped graph identity to an item; never closes an item based on task success. Preserve lineage/evidence summaries; derive facts only with evidence. Feature attribution is never guessed from a title or unknown task kind. Disabled mode means no new inferred cards/spans or automatic lifecycle advancement.

An ungraphed retained broker attempt without an explicit item creates a stable `task` record keyed
by canonical Project plus task id. Replays reuse that identity; later explicit binding transfers
its task-scoped facts to the chosen item without duplicating delivery or accounting. This fallback
uses the ordinary item capacity and reports a refused import when capacity is exhausted.

Automatic outcomes expose `status` (`accepted`, `unchanged`, `partial`, `refused`, `unavailable`), `acceptedCount`, `droppedCount`, `persisted` and an optional typed `reason`. The adapter publishes incomplete ingestion through `board.source.ingestion`, qualifies affected Project usage, and keeps successful `observedAt` separate from `attemptedAt`. A failed attempt retries after the bounded refresh interval; it is never stamped successful merely because its read completed.

### Canonical Program Plan boundary

`plan_structure` is the single Store write for a canonical top-level Epic Program. Its closed
schema names the Program key, monotonically versioned plan and predecessor, graph, destination,
versioned canonical Cloud planning document, bounded logical nodes, gates, capabilities,
dependencies, and file claims. The Store parses every row, checks duplicate logical and graph-node
keys, references, bounds, the complete DAG, document supersession, item capacity, and graph-index
collisions before changing its draft. Success creates or upserts node items, adds the document,
updates graph ownership, advances one Board revision, and performs one atomic persist.

Node logical keys are durable identities: a later version preserves their item IDs and updates
only declared bounded fields. Omitting an old node, gate, or capability never deletes it, but a gate
or capability omitted from the current version is stale evidence: stale gates stay blocked and
cannot be approved, while stale capabilities project `unknown`. A new Plan version clears all prior
gate decisions. `approve_program_gate` binds the named authority's decision to a gate imported by
the exact current Plan version; import cannot carry a decision. Missing capability observations
yield `unknown`; unsupported capabilities, incomplete dependencies, pending gates, and colliding
ready claims yield `blocked`. The only planning states are `planning_ready`,
`blocked`, and `unknown`. The frontier is explicitly `advisory_only`, has no critical-path claim,
and cannot authorize broker execution.

`program_binding` is accepted only from the process-bound workflow producer. It binds one durable
run, Session/provider/process generation and requested Program to the exact current
Program/Plan/graph/node tuple, then returns a stable receipt id, requested classification/item,
explicit `reused_imported_program_node` resolution, effective child item, revision, settlement time
and replay provenance. Historical receipts remain durable across successor Plans without matching a
different current Plan/node request. Broker
ingestion uses that same graph-node index and refuses an unknown Program node instead of falling
back to a title or Program container. Program imports, gate decisions, binding receipts and
document references do not reconcile lifecycle or change scope/evidence/landing/acceptance.

Intentional standard mode and an unavailable Board store are different states. On a typed Board
unavailability refusal, a transport carrying both baseline Project readers may fall back to them
with a visible warning; it must not change the persisted setting. A transport without those readers
preserves the typed refusal. Authentication and routing refusals do not silently select a fallback.

Graph fallback keys include Project identity. The first explicit binding replaces an inferred
fallback. Node-to-item bindings are persisted separately from the primary graph fallback; different
nodes may name different items. Conflicting assignments of the same node report
`graph_node_binding_conflict` without moving that node. Task accounting remains item-owned. Epic
`related` links may reference other Projects; only unambiguous owning-Project membership can resolve
a foreign Epic task binding. The mappings and conflict coverage survive reload and replay.

Use `requestId` (nonempty bounded string) and `expectedRevision` on every command, store revision global CAS. Duplicate request with identical normalized body replays receipt; same id different body conflicts. Actor part of identity. Validate closed per-operation keys. User-editable evidence notes must not mint trusted verification/landing. Disabling works even with live work, preserves histories, stops board-only new operations. Board unavailable errors must not block baseline dispatch.

Published Store headers and materialized seeds are one monotonic revision domain. A seed is published
while its writer generation still owns the Store; it cannot overwrite a later durable header or
restore an earlier mode. A successful mutation publishes its header only after the atomic write and
`fsync` complete.

## JSON view

`board`: `schemaVersion:1`, `revision:Int`, `enabled:Bool`, `mode:"board"|"standard"`, `entitlement:{state:"free_preview",label:"Currently free"}`, `projects:[{id,name,itemCount}]`, `items:[Item]`, `item:Item|null` (selected detail), `truncated:Bool`, `updatedAt:unix seconds`. Transport adds `viewer:{id,canWrite,canManage}` on reads and command replies. Local capabilities include the remote-write switch; UI gates do not replace server authority.

Every read also carries `readState:{status,revision,observedAt,attemptedAt,refreshing,error}`. Top-level
`board.revision` is the latest durable command/CAS revision; `readState.revision` is the revision of
the returned model body. If the header is newer, the body is `stale` and retains its older revision.
A durable OFF startup seed is `ready` read-only retained history when its revision matches the
header; OFF does not infer Projects, replay broker history, scan usage, or advance lifecycle.
An unknown scoped Project is `status:"error"` with `error.code:"project_not_found"` and explicit
unknown/incomplete scope, never an authoritative empty Project.

Item fields: `id`, `key` (human readable), `projectId`, `title`, `type`, `state`, `summary`, `owner` (readable name/id string), `parentId` nullable, `createdAt`, `updatedAt`, `checklist:[{id,title,status,required,evidenceId?}]`, `milestones:[{id,title,status}]`, `artifacts:[{id,title,url,kind}]`, `documentReferences:[{id,documentId,version,title,url,purpose,status,supersedesId?,supersededBy?,addedAt,actor,authority:"narrative_only"}]`, `links:[{id,kind,targetId,label}]`, `obligations:[{id,title,owner,blocking,resolved,actorKind,requiredAction?,blockingScope?,resolutionEvidence?,supersededBy?}]`, `history:[{id,at,actor,kind,summary}]`, `spans:[{id,sessionId,phase,startedAt,endedAt}]`. Include trustworthy findings/verification/landing separately from user claims. Item `usage` may be absent (unknown, never zero); root enriches from UsageLedger.

`documentReferences.purpose` is `plan`, `decision` or `reference`. `documentId` is the stable logical
identity; versions are immutable positive integers beginning at 1, and every later version names the
one current predecessor. URLs are strict canonical hosted-reader URLs rooted at
`https://app.clawdline.com/` with the whole document locator in the fragment. No body is included in
a Board snapshot. Schema-v1 stores without this optional collection decode unchanged. Generic
`artifacts.kind:"document"` rows remain output artifacts and never become planning references.
Both producers measure machine/Session identity (128) and relative path (512) in UTF-8 bytes. Swift
reads `percentEncodedFragment` and performs one URL-form decode equivalent to `URLSearchParams`:
split fields before decoding, convert `+` to space, and decode valid percent triplets exactly once.
Actual decoded traversal is refused; a literal percent-encoded spelling is not decoded a second time.
The first detail page retains current logical heads first, then newest historical versions.

Selected detail adds bounded `remainingWork:{work,userDecisions,workCount,userDecisionCount,
workOmittedCount,userDecisionOmittedCount}`. Rows name their source kind (`checklist`, `child` or
`obligation`), owner, original status/progress and a disposition that preserves required, optional,
canceled and unknown states. Children are the existing `parentId` members; no deeper hierarchy is
introduced. Only explicit obligation `actorKind:"user"` enters `userDecisions`; missing actor kind
projects as `unknown`.

Checklist and obligation rows, including their `remainingWork` projections, carry nullable
`supplementRelationId` (`wfs-` plus 64 lowercase hexadecimal characters). Only the internal
workflow producer may assign it when materializing a new checklist supplement; public Board
commands cannot forge it, even with evidence-writing authority. It identifies one persisted
supplement event, not a title or an entire Session. Readers may fold a same-item checklist and
obligation only when the non-null IDs match exactly and the relation is one-to-one, preserving
both statuses, source rows and any user decision. Missing, legacy or ambiguous identities remain
separate rows. This relation grants no verification, completion or handoff authority.
Blocking work and every explicit user decision are mandatory first-screen rows; stable nonblocking
rows fill the remaining budget. Omitted rows are actionable through selectors
`collection:<item-id>:remaining_work:<offset>`,
`collection:<item-id>:user_decisions:<offset>` and
`collection:<item-id>:document_references:<offset>`. Each reply carries
`board.collection:{kind,itemId,offset,rows,totalCount,nextOffset}` and remains under the normal final
wire limit. Materialization constructs `childrenByParentID` and the progress cache once, then uses
those same dictionaries for summaries, details and remaining-work rows.

`listSummary:{coverage:"complete",group,attention}` is a bounded materialized display projection
shared by compact cards and selected details. `attention` counts unresolved `blockingObligations`,
explicit `userDecisions` (including nonblocking choices), `blockingFindings` and
`failedVerifications` from the complete retained item, not a truncated detail prefix. Missing
actor kind stays unknown; titles do not establish a user decision. No raw obligation/remaining-work
arrays are added to list payloads.

The exclusive display groups are `active`, `planning`, `waiting`, `history`, `completed`, `canceled`
and `coordination`. Existing active/terminal/coordination progress takes precedence. Non-active
blocked progress or attention stays waiting; otherwise unstarted planning/backlog stays planning.
A required future checklist is scope, not a present blocker. Active planning is still declared
activity, never proof of implementation. This classification changes no lifecycle or evidence.
Project `summary.listGroups` counts this same partition over all retained items, including zeros;
legacy `summary.waiting` remains the old combined waiting/planning count for compatibility.

The reader displays separate planning/waiting totals only when the complete category model and
summary coverage are present, never by subtracting partial loaded rows from a project total.
Section counts distinguish loaded/search matches from project totals; search covers loaded items
only. On an older compact response, omitted obligations cannot certify pure planning. Detailed
legacy rows with a complete obligation array may retain the old presentation fallback.

Project list cards are an allowlisted compact projection, not hidden detail envelopes. They omit
nested evidence, history, links, spans and checklist/milestone rows, replacing the latter with
`cardSummary.checklist:{total,completed,required,requiredCompleted,retained,omitted,coverage}` and
`cardSummary.milestones:{total,completed,retained,omitted,coverage}`. Selected detail retains the
bounded underlying collections. Truncated lists report their reason and omitted item count.

Optional `presentation:{authority:"narrative_only",variants:[...]}` contains up to three language
variants with `locale,title,summary,outcome,nextStep,model,authoredAt,status` (`current` or `stale`).
The internal candidate/write seam uses a source fingerprint and mode-generation fence; it is not
a public command and grants no evidence authority. Presentation writes persist atomically without
changing item scope, lifecycle, history or work-update time. Readers use only the current matching
language. Schema-v1 records without this optional field decode unchanged.

Epic `deliveryLanes` exposes up to 32 related non-Epic implementation summaries, including own
Project identity, owner, progress and checklist counts; `deliveryLaneCount` states the total.
Cross-Project navigation must carry the lane's Project, not the enclosing Epic's Project.

`completionReport` is always explicit: `{status:"absent"}` when none exists. Summary/card items expose
only report metadata (`id`, `version`, `status`, `authoredAt`, `actor`, `authorship`, optional `model`,
`scopeRevision`, `itemStateAtAuthorship`, optional `reportBoundary`, `sourceCount`). Selected detail
additionally exposes the five body fields and `sourceReferences`. Every source keeps the stored
`resolution` for compatibility and adds `relationship:<resolution>_at_authorship` plus `resolvedAt`;
an older row without `resolvedAt` uses its report's `authoredAt`. GET never recomputes source relation
against present links.

`completionReportHistory` is metadata counts on cards and retained superseded-version provenance in
normal detail. Superseded bodies remain durable and are fetched one at a time with the selected item
through `report=<opaque-report-id>`; `board.reportSelection` contains only that selected body. Current
report status requires the exact scope and either closed/current authoritative landed ordinary work,
or a typed Coordination boundary; otherwise it is `historical_needs_update`.
Schema-v1 stores with no report fields decode unchanged.

Evidence uses plural arrays `findings`, `verifications`, `landings`, `artifactAcceptances`, `evidenceSummaries`, with `currentEvidence` pointers. The browser displays receipt identity, subject, status and current/historical distinction; artifact acceptance is not a mutable `artifact.accepted` boolean. Bounded projections expose per-collection `projection` counts (`retainedCount`, `omittedCount`, `reason`), not silent data loss. The HTTP service caps the final encoded, enriched response at 2 MiB for both local and Cloud callers.

Types: `feature`, `refactor`, `task`, `bug`, `coordination`, `epic`.

Human item keys such as `CLA-369` are labels, never lookup or ownership authority. Links must
carry the exact machine, Project and item UUID, for example
`https://app.clawdline.com/#page=board&machine=<machine>&project=<project>&item=<item-uuid>`.
The visible label may be the short key; clients must not guess a UUID from a title or ambiguous key.
Item projections include `keyStatus:"unique"|"ambiguous"`, scoped to the exact Project. Existing
duplicate keys remain attached to their original UUIDs and histories; this field is a diagnostic,
not permission to merge, rename, or promote those items.

Each stored Project now has an optional schema-v1 `itemKeyHighWater` ordinal. Creation, broker
fallback creation and atomic Program imports reserve from the same draft-owned sequence, persisted
with the successful mutation. Retiring an inferred card or renaming a Project never resets it;
failed persistence and exact request replay do not consume another ordinal. Older files seed this
watermark in memory from retained numeric key suffixes before any retirement; reads do not rewrite
the file, and the next successful write persists it. Previously retired keys absent from an old
file cannot be reconstructed by guessing. Retained legacy collisions are reported, not silently
repaired. Invalid negative watermarks fail closed on load; numeric exhaustion refuses new items
with `item_key_exhausted` rather than wrapping or lowering the counter. This adds no lifecycle,
verification, assignment, or landing authority.

States: `backlog`, `planning`, `ready`, `execution`, `verified`, `integrated`, `closed`, `canceled`. Unknown historical state must remain explicit if imported.
The browser renders the separate `progress` projection, not a manually advanced status field.
It carries `state`, evidence-based `reason`/`basisCodes`, procedural `warningCodes`,
`lifecycleApplicable`, activity/history flags and bounded attempt/evidence counts. `group` is one of
`active`, `waiting`, `history`, `completed`, `canceled`, `coordination`; unresolved historical
delivery is not active work. A retained successful Task with broker `nothing_to_land` settlement
can project `state:"settled"` without landing evidence. A later read-only settled attempt does not
invalidate prior authoritative landing. Coordination has
`lifecycleApplicable:false`: its context is an interval or handoff, never a completed/open lifecycle.
An authoritative later landing can display landed despite missing older procedural records; those
records qualify the evidence as warnings, not a stale state gate. New work retains previous landings.

Optional `typeDetails` is accepted by create/update and returned with the item. Bug supports only
`rootCause` and `lessons`; Coordination supports only `outcomes`, `difficulties`, `improvements`.
Values are bounded strings. Other types do not accept these fields. Missing narratives are shown
as missing, not inferred from task titles, and do not override observed delivery.
Phases: `planning`, `output`, `review_testing`, `correction`, `integration`; missing = undeclared.
Checklist status: `todo`, `doing`, `passed`, `failed`, `not_applicable`.

## Commands

Common fields: `operation`, `requestId`, `expectedRevision`; item operations use `itemId`.

- `set_enabled`: `enabled:Bool`. Always permitted by store; root transport requires admin. Close open spans at suspension boundary; resume never invents suspended usage.
- `set_ai_consent`: `enabled:Bool`, `provider:"codex"|"claude"`, `policy:"board-reading-v1"`. Requires admin and the same revision/idempotency contract. Independent of Board mode; new and migrated stores default to no consent. Consent is provider-specific and revocable. Changing it invalidates pending narrative writes without changing item lifecycle. Reads expose `narrativeConsent` and `viewer.narrativeProvider`; enabling Board never grants AI consent.
- `create`: `projectId`, `title`, `type`, optional `summary`, `owner`, `parentId`. Return itemId. Project must exist.
- `update`: `itemId`, optional `title`, `summary`, `owner`, `type` (validate parent/container rules).
- `transition`: `itemId`, `state`, optional `note`. Verified/integrated/closed require type-appropriate evidence; artifact Tasks can close with artifact + accepted evidence, never counterfeit Git landing. Trusted caller bool only for backend evidence ingestion, root must enforce credential/role.
- `checklist`: `itemId`, `title` for add; or `checklistId`, `status` for update. Optional `required`.
- `milestone`: `itemId`, `title` for add; or `milestoneId`, `status` for update.
- `artifact`: `itemId`, `title`, `url`, `kind` (`document`, `website`, `deployment`, `commit`, `other`). Only http(s) URLs; no executable schemes. Source artifacts are references, not proof by themselves.
- `document_reference`: `itemId`, `title`, `url`, `purpose` (`plan`, `decision`, `reference`), stable
  `documentId`, positive `version`, and `supersedesId` after version 1. The URL must be the canonical
  `app.clawdline.com` hosted-reader locator. This command appends narrative metadata and history but
  never changes scope revision, lifecycle or current evidence.
- `link`: `itemId`, `kind` (`session`, `task`, `worktree`, `related`, `blocks`, `coordinates`), `targetId`, `label`. Links not cost allocation; item relation cycles refused.
- `obligation`: `itemId`, `title`, `owner`, `blocking:Bool`, optional `actorKind`
  (`user`, `agent`, `external`), `requiredAction` and `blockingScope`. Missing actor kind remains
  unknown for compatibility.
- `resolve_obligation`: `itemId`, `obligationId`, `note`, optional `resolutionEvidence` and
  same-item `supersededBy` obligation id.
- `span`: `itemId`, `sessionId`, `phase`; one active per session. Timestamp labels are declarations, not exact measured token boundaries. Root will join only proven usage boundaries; unknown usage remains unknown.
- `end_span`: `itemId`, `sessionId`, `startRequestId`, `note`. End only the declaration
  made by this command actor under that exact start request. New declared spans have a stable
  actor/request identity. Foreign, broker, wrong-item and unidentifiable legacy spans refuse
  `span_identity_unresolved`; an already ended exact span is harmless. Closing an interval is
  bookkeeping allowed on an otherwise closed item, never scope invalidation or lifecycle promotion.
- `handoff`: `itemId`, `owner` (proposed receiver), `note`; records pending transfer, does not close or change effective owner.
- `accept_handoff`: `itemId`, `note`; root must authenticate receiving identity. Atomically transfer owner and preserve item history.
- `assign_session`: `itemId`, `projectId`, `sessionId` (conversation UUID), `provider`
  (`claude` or `codex`), `note`. Records one pending Session proposal with a generated
  id, captured owner and scope; never assumes liveness, sends a message or changes owner.
- `cancel_session_assignment`: `itemId`, `assignmentId`, `note`. Withdraws exactly
  that pending Session proposal; permitted by ordinary Board write authority.
- `decide_session_assignment`: internal workflow producer only; `itemId`, `projectId`,
  `assignmentId`, `decision` (`accepted`/`declined`), `note`. Direct HTTP, including
  machine-token callers, cannot provide the in-process origin. The actor must equal
  the pending receiver's process-bound workflow provider/conversation. Acceptance
  also compares captured owner/scope and refuses terminal work; decline never
  changes owner. A same-request replay is idempotent and a different proposal id
  fails closed. `accept_handoff` cannot bypass this typed receiver check.

Typed pending `handoff` objects additionally expose `provider`, `scopeRevision` and
`status:pending`. `sessionAssignment` is null or the last settled record with `id`,
`fromOwner`, `proposedOwner`, `provider`, `scopeRevision`, `status`, `proposedAt`,
`settledAt`, `settledBy`. Settlements also append bounded history including the exact
proposal id and participants. Proposal, decision and cancellation bypass lifecycle
promotion; they confer no verification or landing authority. Mac identity is the
authenticated target Store, not an untrusted body field. Offline/busy availability
is not inferred; there is no implicit terminal command or notification.
- `record_report`: ordinary items only when closed or their current progress is authoritatively
  landed; `objective`, `deliveredOutcomes`, `verificationLanding`,
  `remainingWork`, `lessons`, `authorship` (`assistant` or `human`), optional assistant `model`, and
  1–32 `sourceReferences:[{kind,targetId,label,url?}]`. Source `kind` is `task`, `artifact`,
  `evidence`, `handoff`, `item` or `external`; URLs are HTTP(S). Store supplies actor/time/version,
  resolves references as authoring-time `same_item`, `same_project` or `unresolved`, and stamps every
  source `authority:narrative_only`. Coordination has no lifecycle eligibility; it must instead add
  `reportBoundary` as either `{kind:"time_interval",label,startedAt,endedAt}` with finite increasing
  bounds, or `{kind:"handoff",label,handoffId}` matching a same-item handoff source. Only a local
  machine-authenticated root may call it. It neither reconciles nor gates lifecycle. Replacement
  appends; the immutable 16-version capacity refuses overflow without eviction.

`accept_artifact` uses administrative user authority (Cloud's existing write authority). `record_evidence` with kind `verification` or `finding` and `record_report` are available only to the local machine credential. Evidence is labeled `root_attestation`, not broker-executed proof; report sources are narrative-only. Public landing evidence is always refused; only broker ingestion supplies it. All operations still require closed schemas, revision and request identity. Version/capability checks must not pretend unavailable backend features exist.

Root Session landing is a separate broker-only producer, not a fabricated Task. The managed
workflow journal binds the broker-verified receipt to an exact current run/item and canonical
repository. Its internal `record_root_landing` command requires an in-process root-landing origin;
public and ordinary workflow callers cannot replay that authority. The durable outbox and Store
request receipts handle uncertain responses. Historical scope and unrelated verification subjects
cannot receive a current landing pointer; no deployment or whole-project completion is inferred.
See [workflow root landing](board-workflow.md#root-session-landing-projection) for gaps and recovery.

## HTTP / Cloud (root)

`GET /v1/board?project=<id>&item=<id>`; add `report=<report-id>` only with the selected item to fetch
one report body. `POST /v1/board` carries command bodies. Authenticated read, send for ordinary
mutation, admin for mode; machine credential allowed. Common service for local and encrypted Cloud.
Cloud `board` uses the same bounded read lane and keeps both selected item and report identities;
`board-command` remains the action. Opaque IDs; root registers canonical Projects from known start
places / tasks. Unknown reports return `report_not_found`; cross-item and cross-Project selectors
return `report_item_mismatch` and `report_project_mismatch` rather than guessing.

Both local and verified Cloud requests authenticate, validate bounded bodies/queries, and capture
request identity on `RemoteServer`'s owner, then leave it. Commands enter a bounded single-writer
lane with typed `board_command_busy` saturation; an identical in-flight actor/requestId joins one
durable result and different content conflicts.
`board_command_busy` also bounds identical retries to eight retained replies per command; excess
callers may retry the same identity after draining. Reads enter a separate bounded lane with typed
`board_read_busy` saturation, so response encoding never waits behind Board persistence. Command
success is returned only after durable effect. Neither lane is an interactive terminal, usage, or
general slow-read queue.

Read-model invalidation is a bounded union of `catalog`, `durableModel`, `usage`, and
`modeCatchUp` reasons. One running worker has at most one consolidated successor; later events do
not replace earlier obligations. A successful UsageLedger checkpoint contributes `usage` only after
leaving the ledger owner. Board never owns the Ledger or waits on its commit lock, and failed
checkpoints emit no invalidation.

Accepted broker live-state or child-identity changes publish a captured, credential-free source
record after leaving the registry lock. Creation and terminal/landing receipts are not sufficient:
`spawning` and `briefed` must reach the durable Board without a GET, restart or raw-data rescan.
Refused stale replacements emit nothing. The adapter ingests on its utility lane, not on the
interactive registry owner.

Elapsed time alone does not invalidate a successful model. Failed refreshes have bounded retries;
the current model is still served. Ordinary broker observations do not rescan UsageLedger. A
utility timer compares complete published Session inventory Project paths and admits catalog work
only on changes (or configuration invalidation), never while OFF or on incomplete inventory.
Catalog reads do not build every item's detail or evaluate an unnecessary startup seed.

If a command persisted but its response projection exceeds the byte budget, the typed error explicitly says the command was saved and carries `commandApplied:true`. Projection failure does not masquerade as a successful mode-off snapshot. Ambiguous persistence/network failure continues to require the same request identity on retry.

Browser `api.board(project?, item?) -> envelope` and `api.boardCommand(body) -> envelope`, using selected/owning machine (never arbitrary fleet machine). Mock supports same shape.

A reverse Session selector is a UUID admitted case-insensitively and immediately normalized to
lowercase for the materialized index and response. The reply's `sessionId` is canonical; different
case never creates or selects a second relation.

## Web module contract

Export `bindBoardPage(elements, environment)` returning `{enter,leave,refresh,escape,open,state}`. `elements` has `board`, `board-title`, `board-subtitle`, `board-items`, `board-detail`, `board-status`, `board-search`, `board-back`, `board-refresh`. `environment` has `read(project?,item?)`, `navigate`, `openSession(id,projectPresentation)`, `onMode(board)` and optionally `onProjects(projects)` and injectable timers. The Promise-compatible Session callback receives the selected Project presentation and resolves a durable conversation ID to exactly one live terminal ID; zero or multiple matches return a visible typed refusal. The view does not start resume by itself. Traditional Chinese and English copy is local to the view module. DOM uses safe textContent, not unsanitized HTML.

The Projects page opens the board within one selected Project. Render an overview, visual lifecycle,
scoped search and item cards; unresolved historical records and completed/canceled history are
collapsed separately and render in batches on expansion. Detail leads with
objective and progress, then a concise remaining-work/user-decision split and a prominent five-part
report when present (absence is a collapsed note). Typed Original plan, Decisions & changes and
Reference documents remain distinct from Outputs before progressively disclosing token spending, Session and
worktree relations, evidence and history. No create/edit/transition command is emitted by this view.
Assistants record objectives and facts through the existing authorized API; lifecycle advancement
belongs to the store. Only the settings controller emits a browser mode mutation, using revision
CAS and stable ambiguous-retry identity. Stale reads cannot overwrite newer selection. One bounded
15-second refresh loop belongs to the active, visible board; off mode retains read-only history.
Report text uses DOM `textContent`; only HTTP(S) source links become anchors with opener isolation.
Document references use only a strict canonical `app.clawdline.com` locator and open their existing
encrypted reader on selection; rendering detail does not fetch document bodies. A generic document
artifact stays under Outputs.
Superseded report metadata renders as explicit version controls. Loading, failure and ticket-fenced
late replies are local to that reader and never replace the current selected item/report.

Historical resume is a separate action boundary. The browser persists a bounded pending/unknown
fence keyed by machine, place, provider, conversation and action, plus its original request UUID,
and reuses that UUID in the local idempotency header or encrypted Cloud request after reload. Only
unique matching live inventory or an explicit server refusal clears it. Time, a successful send
response and a successful UI handoff are not execution evidence. Storage failure refuses resume.
Manual browser-storage deletion and atomic cross-tab admission are explicitly outside this version's
guarantee.

## Proof

Store and adapter checks are registered with the Swift suite. `Tests/web-board.mjs` and `Tests/web-board-transport.mjs` exercise the real view, settings controller and transports, including permission, stale-read and ambiguous-retry behavior. New guards require red-before-green proof. Focused Swift checks use `tools/with-compile-lock.sh`; the landing root owns one exact-candidate full suite under the same machine-wide resource lock. See [verification workflow](verification-workflow.md).
