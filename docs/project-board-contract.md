# Project Board implementation contract

Project Board is **enabled by default and currently free**. This page describes the implementation boundaries and wire contract; [Project Board](project-board.md) explains the user workflow. Existing safeguards remain in standard mode.

## Component ownership

- `ProjectBoardStore` owns persistence, mode, work items, lifecycle gates and command receipts.
- `ProjectBoardIntegration` projects trusted broker and UsageLedger facts without transferring their ownership to the board.
- `ProjectBoardHTTP` owns transport authority and the final response byte budget; local and Cloud requests use the same service.
- `view/board.js` renders snapshots and serializes user commands. `input/board-settings.js` owns navigation and settings projection, not a second persisted mode.

## Store boundary (Foundation/CryptoKit only, independently testable)

`final class ProjectBoardStore` with `static let shared`, `init(url: URL)` for isolated tests. Default owned persistence under Application Support/Clawdline, env override `CLAWDLINE_BOARD_STORE` for tests. One serialized owner, atomic JSON durable writes, bounded inputs, reject corrupt/unknown-version store without replacing it. Persistence includes settings, items, immutable history and request receipts; Orchestrator and UsageLedger remain separate owners.

Adapter interface:

- `snapshot(project: String? = nil, item: String? = nil) -> [String: Any]` returns complete envelope `{"board": ...}`. Bounded scan/pagination with honest truncation allowed; expose it.
- `command(_ body: [String: Any], actor: String, trusted: Bool = false) -> Reply` where Reply has `status: Int`, `body: [String: Any]`. Errors body `{"error":{"code":...,"message":...}}`. Success full snapshot plus `itemId` when applicable.
- `ensureProject(id: String, name: String) -> AutomaticMutationOutcome` persists id/name only; the adapter resolves canonical identity. No paths in public record.
- `var enabled: Bool { get }` durable default true. Mode and entitlement owned here, not a second Config boolean.
- `ingest(task: [String: Any], projectID: String) -> AutomaticMutationOutcome` receives trusted broker records; idempotently links explicit `workItemId`, or a Project-scoped graph identity to an item; never closes an item based on task success. Preserve lineage/evidence summaries; derive facts only with evidence. Unknown Feature remains unknown, not automatic per-task cards. Disabled mode means no new inferred cards/spans.

Automatic outcomes expose `status` (`accepted`, `unchanged`, `partial`, `refused`, `unavailable`), `acceptedCount`, `droppedCount`, `persisted` and an optional typed `reason`. The adapter publishes incomplete ingestion through `board.source.ingestion`, qualifies affected Project usage, and keeps successful `observedAt` separate from `attemptedAt`. A failed attempt retries after the bounded refresh interval; it is never stamped successful merely because its read completed.

Graph fallback keys include Project identity. The first explicit binding replaces an inferred fallback; a conflicting later explicit item retains its own task link but reports `graph_binding_conflict` without moving the established fallback. The mapping and conflict coverage survive reload and replay.

Use `requestId` (nonempty bounded string) and `expectedRevision` on every command, store revision global CAS. Duplicate request with identical normalized body replays receipt; same id different body conflicts. Actor part of identity. Validate closed per-operation keys. User-editable evidence notes must not mint trusted verification/landing. Disabling works even with live work, preserves histories, stops board-only new operations. Board unavailable errors must not block baseline dispatch.

## JSON view

`board`: `schemaVersion:1`, `revision:Int`, `enabled:Bool`, `mode:"board"|"standard"`, `entitlement:{state:"free_preview",label:"Currently free"}`, `projects:[{id,name,itemCount}]`, `items:[Item]`, `item:Item|null` (selected detail), `truncated:Bool`, `updatedAt:unix seconds`. Transport adds `viewer:{id,canWrite,canManage}` on reads and command replies. Local capabilities include the remote-write switch; UI gates do not replace server authority.

Item fields: `id`, `key` (human readable), `projectId`, `title`, `type`, `state`, `summary`, `owner` (readable name/id string), `parentId` nullable, `createdAt`, `updatedAt`, `checklist:[{id,title,status,required,evidenceId?}]`, `milestones:[{id,title,status}]`, `artifacts:[{id,title,url,kind}]`, `links:[{id,kind,targetId,label}]`, `obligations:[{id,title,owner,blocking,resolved}]`, `history:[{id,at,actor,kind,summary}]`, `spans:[{id,sessionId,phase,startedAt,endedAt}]`. Include trustworthy findings/verification/landing separately from user claims. Item `usage` may be absent (unknown, never zero); root enriches from UsageLedger.

Evidence uses plural arrays `findings`, `verifications`, `landings`, `artifactAcceptances`, `evidenceSummaries`, with `currentEvidence` pointers. The browser displays receipt identity, subject, status and current/historical distinction; artifact acceptance is not a mutable `artifact.accepted` boolean. Bounded projections expose per-collection `projection` counts (`retainedCount`, `omittedCount`, `reason`), not silent data loss. The HTTP service caps the final encoded, enriched response at 2 MiB for both local and Cloud callers.

Types: `feature`, `refactor`, `task`, `bug`, `coordination`, `epic`.
States: `backlog`, `planning`, `ready`, `execution`, `verified`, `integrated`, `closed`, `canceled`. Unknown historical state must remain explicit if imported.
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

Export `bindBoardPage(elements, environment)` returning `{enter,leave,refresh,escape,open,state}`. `elements` has `board`, `board-projects`, `board-items`, `board-detail`, `board-status`, `board-new`, `board-search`, `board-layout`, `board-type`, `board-state`, `board-back`. `environment` has `read(project?,item?)`, `command(body)`, `navigate`, `openSession(id)`, `onMode(board)` and optionally `onProjects(projects)`. The Session callback resolves a durable conversation ID to exactly one live terminal ID; zero or multiple matches return a visible typed refusal. Traditional Chinese and English copy is local to the view module. DOM uses safe textContent, not unsanitized HTML.

Render Project selection then items (list default, optional board columns), filters/search; detail all checklist/milestone/links/spans/artifacts/obligations/history plus token unknown treatment. Create/edit forms inline not browser prompt; action errors visible. Mutations use current revision and idempotency id; stale async reads cannot overwrite newer selection. Off shows history read-only and standard-mode explanation. Navigation/new command mode handled by root. Mobile no mandatory drag.

## Proof

Store and adapter checks are registered with the Swift suite. `Tests/web-board.mjs` and `Tests/web-board-transport.mjs` exercise the real view, settings controller and transports, including permission, stale-read and ambiguous-retry behavior. New guards require red-before-green proof. Focused Swift checks use `tools/with-compile-lock.sh`; the landing root owns one exact-candidate full suite under the same machine-wide resource lock. See [verification workflow](verification-workflow.md).
