# Project Board implementation contract

Project Board is **enabled by default and currently free**. This page describes the implementation boundaries and wire contract; [Project Board](project-board.md) explains the user workflow. Existing safeguards remain in standard mode.

## Component ownership

- `ProjectBoardStore` owns persistence, mode, work items, lifecycle gates and command receipts.
- `ProjectBoardIntegration` projects trusted broker and UsageLedger facts without transferring their ownership to the board.
- `ProjectBoardHTTP` owns transport authority and the final response byte budget; local and Cloud requests use the same service.
- `view/board.js` renders evidence-driven, read-only Project snapshots. `input/board-settings.js` owns navigation and the administrative mode command, not a second persisted mode.

## Store boundary (Foundation/CryptoKit only, independently testable)

`final class ProjectBoardStore` with `static let shared`, `init(url: URL)` for isolated tests. Default owned persistence under Application Support/Clawdline, env override `CLAWDLINE_BOARD_STORE` for tests. One serialized owner, atomic JSON durable writes, bounded inputs, reject corrupt/unknown-version store without replacing it. Persistence includes settings, items, immutable history and request receipts; Orchestrator and UsageLedger remain separate owners.

Adapter interface:

- `snapshot(project: String? = nil, item: String? = nil) -> [String: Any]` returns complete envelope `{"board": ...}`. Bounded scan/pagination with honest truncation allowed; expose it.
- `command(_ body: [String: Any], actor: String, trusted: Bool = false) -> Reply` where Reply has `status: Int`, `body: [String: Any]`. Errors body `{"error":{"code":...,"message":...}}`. Success full snapshot plus `itemId` when applicable.
- `ensureProject(id: String, name: String) -> AutomaticMutationOutcome` persists id/name only; the adapter resolves canonical identity. No paths in public record.
- `var enabled: Bool { get }` durable default true. Mode and entitlement owned here, not a second Config boolean.
- `ingest(task: [String: Any], projectID: String) -> AutomaticMutationOutcome` receives trusted broker records; idempotently links explicit `workItemId`, or a Project-scoped graph identity to an item; never closes an item based on task success. Preserve lineage/evidence summaries; derive facts only with evidence. Feature attribution is never guessed from a title or unknown task kind. Disabled mode means no new inferred cards/spans or automatic lifecycle advancement.

An ungraphed retained broker attempt without an explicit item creates a stable `task` record keyed
by canonical Project plus task id. Replays reuse that identity; later explicit binding transfers
its task-scoped facts to the chosen item without duplicating delivery or accounting. This fallback
uses the ordinary item capacity and reports a refused import when capacity is exhausted.

Automatic outcomes expose `status` (`accepted`, `unchanged`, `partial`, `refused`, `unavailable`), `acceptedCount`, `droppedCount`, `persisted` and an optional typed `reason`. The adapter publishes incomplete ingestion through `board.source.ingestion`, qualifies affected Project usage, and keeps successful `observedAt` separate from `attemptedAt`. A failed attempt retries after the bounded refresh interval; it is never stamped successful merely because its read completed.

Intentional standard mode and an unavailable Board store are different states. On a typed Board
unavailability refusal, a transport carrying both baseline Project readers may fall back to them
with a visible warning; it must not change the persisted setting. A transport without those readers
preserves the typed refusal. Authentication and routing refusals do not silently select a fallback.

Graph fallback keys include Project identity. The first explicit binding replaces an inferred fallback; a conflicting later explicit item retains its own task link but reports `graph_binding_conflict` without moving the established fallback. The mapping and conflict coverage survive reload and replay.

Use `requestId` (nonempty bounded string) and `expectedRevision` on every command, store revision global CAS. Duplicate request with identical normalized body replays receipt; same id different body conflicts. Actor part of identity. Validate closed per-operation keys. User-editable evidence notes must not mint trusted verification/landing. Disabling works even with live work, preserves histories, stops board-only new operations. Board unavailable errors must not block baseline dispatch.

## JSON view

`board`: `schemaVersion:1`, `revision:Int`, `enabled:Bool`, `mode:"board"|"standard"`, `entitlement:{state:"free_preview",label:"Currently free"}`, `projects:[{id,name,itemCount}]`, `items:[Item]`, `item:Item|null` (selected detail), `truncated:Bool`, `updatedAt:unix seconds`. Transport adds `viewer:{id,canWrite,canManage}` on reads and command replies. Local capabilities include the remote-write switch; UI gates do not replace server authority.

Item fields: `id`, `key` (human readable), `projectId`, `title`, `type`, `state`, `summary`, `owner` (readable name/id string), `parentId` nullable, `createdAt`, `updatedAt`, `checklist:[{id,title,status,required,evidenceId?}]`, `milestones:[{id,title,status}]`, `artifacts:[{id,title,url,kind}]`, `links:[{id,kind,targetId,label}]`, `obligations:[{id,title,owner,blocking,resolved}]`, `history:[{id,at,actor,kind,summary}]`, `spans:[{id,sessionId,phase,startedAt,endedAt}]`. Include trustworthy findings/verification/landing separately from user claims. Item `usage` may be absent (unknown, never zero); root enriches from UsageLedger.

Evidence uses plural arrays `findings`, `verifications`, `landings`, `artifactAcceptances`, `evidenceSummaries`, with `currentEvidence` pointers. The browser displays receipt identity, subject, status and current/historical distinction; artifact acceptance is not a mutable `artifact.accepted` boolean. Bounded projections expose per-collection `projection` counts (`retainedCount`, `omittedCount`, `reason`), not silent data loss. The HTTP service caps the final encoded, enriched response at 2 MiB for both local and Cloud callers.

Types: `feature`, `refactor`, `task`, `bug`, `coordination`, `epic`.
States: `backlog`, `planning`, `ready`, `execution`, `verified`, `integrated`, `closed`, `canceled`. Unknown historical state must remain explicit if imported.
The browser renders the separate `progress` projection, not a manually advanced status field.
It carries `state`, evidence-based `reason`/`basisCodes`, procedural `warningCodes`,
`lifecycleApplicable`, activity/history flags and bounded attempt/evidence counts. Coordination has
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
- `create`: `projectId`, `title`, `type`, optional `summary`, `owner`, `parentId`. Return itemId. Project must exist.
- `update`: `itemId`, optional `title`, `summary`, `owner`, `type` (validate parent/container rules).
- `transition`: `itemId`, `state`, optional `note`. Verified/integrated/closed require type-appropriate evidence; artifact Tasks can close with artifact + accepted evidence, never counterfeit Git landing. Trusted caller bool only for backend evidence ingestion, root must enforce credential/role.
- `checklist`: `itemId`, `title` for add; or `checklistId`, `status` for update. Optional `required`.
- `milestone`: `itemId`, `title` for add; or `milestoneId`, `status` for update.
- `artifact`: `itemId`, `title`, `url`, `kind` (`document`, `website`, `deployment`, `commit`, `other`). Only http(s) URLs; no executable schemes. Source artifacts are references, not proof by themselves.
- `link`: `itemId`, `kind` (`session`, `task`, `worktree`, `related`, `blocks`, `coordinates`), `targetId`, `label`. Links not cost allocation; item relation cycles refused.
- `obligation`: `itemId`, `title`, `owner`, `blocking:Bool`.
- `resolve_obligation`: `itemId`, `obligationId`, `note`.
- `span`: `itemId`, `sessionId`, `phase`; one active per session. Timestamp labels are declarations, not exact measured token boundaries. Root will join only proven usage boundaries; unknown usage remains unknown.
- `handoff`: `itemId`, `owner` (proposed receiver), `note`; records pending transfer, does not close or change effective owner.
- `accept_handoff`: `itemId`, `note`; root must authenticate receiving identity. Atomically transfer owner and preserve item history.

`accept_artifact` uses administrative user authority (Cloud's existing write authority). `record_evidence` with kind `verification` or `finding` is available only to the local machine credential and is labeled `root_attestation`, not broker-executed proof. Public landing evidence is always refused; only broker ingestion supplies it. Both operations still require closed schemas, revision and request identity. Version/capability checks must not pretend unavailable backend features exist.

## HTTP / Cloud (root)

`GET /v1/board?project=<id>&item=<id>`; `POST /v1/board` command body. Authenticated read, send for ordinary mutation, admin for mode; machine credential allowed. Common service for local and encrypted Cloud. Cloud `board` read with machine request channel and `board-command` action. Opaque IDs; root registers canonical Projects from known start places / tasks.

If a command persisted but its response projection exceeds the byte budget, the typed error explicitly says the command was saved and carries `commandApplied:true`. Projection failure does not masquerade as a successful mode-off snapshot. Ambiguous persistence/network failure continues to require the same request identity on retry.

Browser `api.board(project?, item?) -> envelope` and `api.boardCommand(body) -> envelope`, using selected/owning machine (never arbitrary fleet machine). Mock supports same shape.

## Web module contract

Export `bindBoardPage(elements, environment)` returning `{enter,leave,refresh,escape,open,state}`. `elements` has `board`, `board-title`, `board-subtitle`, `board-items`, `board-detail`, `board-status`, `board-search`, `board-back`, `board-refresh`. `environment` has `read(project?,item?)`, `navigate`, `openSession(id)`, `onMode(board)` and optionally `onProjects(projects)` and injectable timers. The Session callback resolves a durable conversation ID to exactly one live terminal ID; zero or multiple matches return a visible typed refusal. Traditional Chinese and English copy is local to the view module. DOM uses safe textContent, not unsanitized HTML.

The Projects page opens the board within one selected Project. Render an overview, visual lifecycle,
scoped search and item cards; landed/canceled history is collapsed separately. Detail leads with
objective, progress, blockers and outputs, then progressively discloses token spending, Session and
worktree relations, evidence and history. No create/edit/transition command is emitted by this view.
Assistants record objectives and facts through the existing authorized API; lifecycle advancement
belongs to the store. Only the settings controller emits a browser mode mutation, using revision
CAS and stable ambiguous-retry identity. Stale reads cannot overwrite newer selection. One bounded
15-second refresh loop belongs to the active, visible board; off mode retains read-only history.

## Proof

Store and adapter checks are registered with the Swift suite. `Tests/web-board.mjs` and `Tests/web-board-transport.mjs` exercise the real view, settings controller and transports, including permission, stale-read and ambiguous-retry behavior. New guards require red-before-green proof. Focused Swift checks use `tools/with-compile-lock.sh`; the landing root owns one exact-candidate full suite under the same machine-wide resource lock. See [verification workflow](verification-workflow.md).
