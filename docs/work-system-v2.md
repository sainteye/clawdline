# Work system v2

Status: **implemented locally; release verification and live-data cutover pending**. The owner
approved the defaults in this document on 2026-09-22. The v2 store, authority boundaries,
lifecycle API, Board Console, Session projection, direct to-dos, proposals, and guarded v1 reset
are present on the implementation branch. The person's running daemon has not been changed and
its v1 rows have not been deleted.

This page is normative for the replacement. Where the earlier board design, work-system design,
or broker projection disagrees with it, this page wins for v2. The acceptance contract is
[`work-system-v2-acceptance.md`](work-system-v2-acceptance.md).

The implementation surface is `/v1/work/v2/`. Person writes require a send-capable device;
Agent writes are isolated under `/v1/work/v2/agent/`. The old background Board sweep is not
started, so a reset store remains empty instead of being repopulated by inferred digests or
proposals. `GET /v1/work/v2/reset` is a local-only dry run. Its count-bound confirmation is
required by the local-only POST that irreversibly clears v1 work tables while preserving broker,
landing, Session, settings, Git, and v2 records.

## 1. Purpose

The work system has one human-visible subject: a **work item deliberately created by a person**.
It answers five questions without inventing work on the person's behalf:

1. What Project does this belong to?
2. What kind of work is it?
3. Which Session owns it now?
4. Which lifecycle stage has the owning Agent reached?
5. What evidence, documents, and local steps support that answer?

The broker is not a second author of work. It is the execution and evidence ledger: it opens and
identifies Sessions, dispatches children, records reliable effects, validates Git landing evidence,
and projects an assigned item into its owner's Session to-do panel.

## 2. Non-negotiable invariants

1. **Only a person creates a work item.** Device-authenticated UI/API actions are the only creation
   path. The machine/orchestrator credential and task secrets cannot create one.
2. **Only a person assigns, reassigns, unassigns, deletes/cancels, or reopens an item.** An Agent may not
   appoint itself or another Session.
3. **An Agent may propose, never promote.** A proposal is a previewable draft. It becomes a work
   item only after a person accepts it, possibly after editing it.
4. **One item has at most one owning Session at a time.** One Session may own several items.
5. **The owning Agent advances execution.** It may edit the title and description, maintain
   documents and item-local steps, and request lifecycle transitions. It may not change Project,
   kind, owner, or deletion/cancellation state.
6. **The broker validates; it does not infer authorship.** Dispatches, landings, clocks, and task
   results never create, assign, defer, close, or delete a work item by themselves.
7. **A Session responsibility is one row per work item, not one row per dispatch.** Child tasks are
   execution detail nested under the item.
8. **Quick Session to-dos are not work items.** They are a direct, lightweight queue for one
   Session and cannot silently appear on the Board.
9. **Unknown never advances or removes work.** An unreadable Session, Git state, deployment proof,
   or store row preserves the last durable state and reports the gap.
10. **Every mutation is idempotent and versioned.** A stale editor receives a typed conflict and
    must reread; a retry never applies the same external effect twice.

## 3. Projects and icons

A work item names a Project selected from the daemon's Project catalog. Creation never accepts an
arbitrary free-form path as a Project.

The durable item stores the Project identity and a path snapshot needed to explain historical
events. Its read model joins the current catalog presentation:

- Project label;
- canonical/display path;
- Project icon from the existing icon registry;
- whether the Project can currently be resolved.

The icon is not copied into every item row. Every Board card and assigned-item row in a Session
to-do panel carries the joined icon. An unresolved Project keeps its identity and shows a typed
`project_unavailable` presentation; it is not reassigned to a similarly named directory.

## 4. Kinds and placement

The closed kind vocabulary is:

| Kind | Initial area | Assignable in v2.0 | Meaning |
| --- | --- | --- | --- |
| `feature` | Unassigned | yes | A user-visible capability or coherent product change |
| `issue` | Unassigned | yes | A defect or bounded problem to correct |
| `epic` | Planning | no | A container or direction that may later yield executable items |
| `refactor` | Planning | no | A planned structural improvement, not yet execution work |
| `plan` | Planning | no | A saved plan or investigation direction |

Planning items are retained and editable but do not acquire an owner or enter the execution
lifecycle in v2.0. Later work may create linked Feature/Issue items; it must not silently mutate a
planning item into executable work.

The local `issue` kind is not a GitHub Issue and does not call GitHub. Any future external link is
an explicit reference, not a synchronization contract.

## 5. Work-item record

The durable identity contains at least:

- `id`: lowercase UUID;
- `project_id` and Project path snapshot;
- `kind`;
- `title` and Markdown `description`;
- lifecycle `phase` and optional condition;
- deployment policy;
- creation actor and timestamps;
- current version;
- cancellation/closure facts when terminal.

Related records are separate so frequently read cards do not decode unbounded text:

### 5.1 Assignments

An append-only assignment history records:

- item;
- target mode (`existing_session` or `new_session`);
- conversation id when known;
- terminal id, assistant, and model as observations;
- `assigning`, `active`, `released`, or `failed` state;
- human actor and timestamps;
- failure/refusal evidence.

The current owner is the one active assignment. A failed attempt is history, not an owner.

### 5.2 Documents

An item may have ordered document references with a closed role such as `spec`, `design`, `test`,
`deploy`, or `other`. A document may carry Markdown content or a repository-relative path/URL with
a summary. The system does not copy an entire repository document merely to show that it exists.

### 5.3 Steps

Item-local steps are a lightweight Agent aid:

- ordered title;
- `todo` or `done`;
- creator and completion facts;
- version.

They do not create Board items, do not assign Sessions, and do not automatically advance the
item's lifecycle. A completed item may retain its steps as history.

### 5.4 Reference images

A person may attach up to six raster reference images to a nonterminal item. The daemon decodes
and normalizes each accepted source to PNG before storage; a filename, dimensions, byte count,
position, actor, and timestamp remain as metadata. Board list reads carry only that metadata. Image
bytes are fetched on demand through an opaque item-image id, including through a dedicated
machine-scoped Cloud read, so an unassigned or Planning item does not need a Session identity to
show its references.

Reference images are person-owned input. Agents may read them with the item briefing and item
view, but cannot add, replace, order, or delete them. Adding or deleting one is an optimistic,
idempotent item mutation that increments the item version and appends an immutable event. A
terminal item must be reopened before its references change.

### 5.5 Events

Every accepted mutation appends one typed event naming actor, subject, previous version, next
version, evidence, and time. Events are immutable. Display state is read from the durable item and
its evidence; a periodic reconciliation loop does not write cosmetic history.

## 6. Lifecycle

Executable items follow this main path:

```text
created -> assigning -> assigned -> implementing -> verifying
        -> merging -> deploying -> done
```

`assigning` is a visible transient state only for a request to open a new Session. An existing live
Session assignment normally moves from `created` to `assigned` in one transaction.

| Phase | Actor that requests it | Required evidence / guard |
| --- | --- | --- |
| `created` | person, by creation | valid Project, executable kind, title and description |
| `assigning` | person | durable new-Session assignment intent recorded before terminal I/O |
| `assigned` | assignment service | exactly one resolved owner; opening/briefing outcome recorded |
| `implementing` | owning Agent | owner identity matches; item is not terminal |
| `verifying` | owning Agent | verification plan or references recorded |
| `merging` | owning Agent | successful verification evidence recorded |
| `deploying` | owning Agent | broker landing, or a direct-Session receipt whose exact commit is contained by both the Project's local target and its remote-tracking target |
| `done` | owning Agent | deployment evidence is valid, or deployment is explicitly not required |
| `cancelled` | person | cancellation reason; assignment released |

The `done` transition records the completing owner, releases the active assignment, and removes
the item from the Session's open-responsibility projection in the same transaction. Its assignment
history and completion evidence remain on the item and in that Session's recent-completion
projection. A cancellation does the same release under the person's authority but is not presented
as successful completion.

An item assigned directly to an existing Session has no broker child task and therefore no child
landing record. Its owning Agent may name an exact commit, local target branch, and remote. The
daemon resolves all three through Git and records a landing receipt only when the commit is on both
the local target and `refs/remotes/<remote>/<target>`. Caller text, a successful build by itself, or
an unpushed local commit never earns the landing check.

The owning-Agent phase request carries the claim as structured input, not prose:

```json
{"next":"deploying","landing":{"commit":"<full commit>","target":"main","remote":"origin"}}
```

The stored event replaces `<full commit>` and both target spellings with Git's resolved object ids.

### 6.1 Rework and failure

- A failed verification returns to `implementing` with the failed check preserved.
- A merge problem returns to `implementing` or `verifying`; prior landing evidence remains history.
- A deployment failure stays in `deploying` with a blocker. It does not become done.
- A completed/cancelled item is reopened only by a person. Reopening starts a new execution cycle
  and never rewrites the earlier one.

### 6.2 Conditions are not phases

`blocked`, `waiting_user`, `owner_required`, `owner_offline`, and `evidence_unknown` are conditions
layered on the current phase. An item may therefore say “verifying · waiting for you” rather than
losing the stage at which it is blocked.

Only the owning Agent may set or clear an Agent condition. The broker derives `owner_offline` and
`evidence_unknown` from typed readings without moving the main phase.

### 6.3 Deployment policy

`deployment_policy` is `required`, `not_required`, or `agent_decides`; new executable items default
to `agent_decides`.

When the policy is `agent_decides`, skipping deployment requires an Agent-authored
`not_applicable` decision with a concrete reason. `done` is refused if neither deploy evidence nor
that decision exists. A person may change the policy; the Agent may not.

## 7. Authority

| Operation | Person/device | Owning Agent | Other Agent | Broker/rule |
| --- | ---: | ---: | ---: | ---: |
| Create item | yes | no | no | no |
| Assign/reassign/unassign/delete (cancel)/reopen | yes | no | no | no |
| Change Project/kind/deployment policy | yes | no | no | no |
| Edit title/description | yes | yes | no | no |
| Add/edit documents and steps | yes | yes | no | no |
| Add/delete reference images | yes | no | no | no |
| Advance/rewind execution phase | no | yes | no | validates only |
| Create proposal | no | yes | yes, attributed to its root | no |
| Accept/reject/edit proposal | yes | no | no | no |
| Create/Send/Delete quick Session to-do | yes | no | no | no |
| Complete quick Session to-do | yes | yes, for itself | no | no |

Person-only writes are served through device-authenticated routes requiring the send capability.
They are not accepted merely because a request carries the orchestrator credential. Agent writes
use the orchestrator surface and expose only the Agent operations in the table.

A person may correct Project or kind only before the first successful assignment. Thereafter those
identity fields are immutable; changing direction means cancelling this item and deliberately
creating another. Deployment policy remains person-editable on a nonterminal item.

The current shared machine credential proves “a Session on this machine”, not which Session made
the request. Until a per-Session capability exists, the thin CLI supplies the conversation id from
the assistant environment and the server revalidates it against the live identity and active
assignment. This is a cooperative ownership boundary, not a sandbox against a deliberately forged
conversation id, and the wire must say so rather than claiming stronger isolation.

## 8. Assignment

### 8.1 Existing Session

The person chooses from current assistant Sessions resolved to the same Project. The picker shows
assistant, label, current work count, and identity-read freshness. An unknown or cross-Project
Session is refused; string similarity is not identity.

The assignment and the Session's assigned-item projection are committed together. A Session
positively observed `idle` may receive a courtesy briefing after that commit. A `working`,
`waiting`, or `unknown` Session receives no terminal input: it pulls the projection from its Agent
to-do API at the next turn boundary. This is an expected queued assignment, not an
`assigned_unnotified` failure. A failed courtesy send to an idle Session leaves the durable
`assigned_unnotified` condition without rolling back ownership.

### 8.2 New Session

The person chooses assistant and model. The assignment service reuses the existing reliable
Root-Assignment mechanics below the API boundary:

1. record intent and item relationship;
2. open the terminal outside the store transaction;
3. wait for the correct assistant composer;
4. type one briefing line at most once;
5. resolve the conversation id;
6. activate the assignment or record a visible failure.

The briefing names the item, Project, objective, description, reference images, documents, steps,
lifecycle commands, and ownership rules. It does not create a child relationship or give the Agent permission to
create another Board item.

For a first assignment, failure returns the item to `created` with `assignment_failed`; the failed
assignment remains in history and no owner is projected.

### 8.3 Reassignment and unassignment

Reassignment to an existing Session changes the active owner and the Session projections in one
transaction. Only a positively idle new owner receives a courtesy briefing; every other state
pulls the new row at its next turn boundary. The prior owner is refused on its next write even if
its Console or turn still has stale state.

Reassignment to a new Session keeps the current owner and lifecycle phase while the replacement is
opening. Only successful identity resolution atomically releases the former assignment and
activates the replacement. A failed replacement attempt therefore leaves the prior owner intact.

Unassignment releases the owner and removes the Session projection in one transaction. It
preserves the lifecycle phase, documents, steps, and evidence and derives an `owner_required`
condition; it does not pretend unfinished implementation was undone. A later assignment resumes
from that recorded phase. Terminal items must be reopened before they can be assigned.

## 9. Broker after v2

The broker remains responsible for:

- Session identity, liveness, and terminal I/O;
- child dispatch and task records;
- receipts, outbox effects, retries, and typed failures;
- Git/worktree and landing evidence;
- assignment briefing delivery;
- evidence validation and read-model projection.

It is no longer responsible for:

- inventing a work item from a dispatch, task, leftover, clock, or landing;
- assigning or reassigning ownership;
- turning inactivity into Backlog movement;
- automatically accepting, closing, or cancelling an item;
- filing automatic proposals;
- treating a dispatch to-do as a top-level Session responsibility.

A task may carry `work_id` to become execution detail of an existing item. An unbound task remains
broker execution history and never materializes as human work. Landing evidence may satisfy a
transition guard, but the owning Agent still requests the transition.

## 10. Agent proposals

A proposal contains Project, proposed kind, title, description, reason, suggested acceptance,
proposing Session, optional source item/quick-to-do, and version. Its states are `pending`,
`accepted`, and `rejected`.

It does not expire into acceptance, does not create a work item through a rule, and is never mixed
with the Board count. The person can preview and edit it before accepting. Acceptance creates one
new item and links the proposal to it in one idempotent transaction. Rejection keeps provenance
without creating work.

## 11. Session to-do panel

The panel has three explicitly labelled groups.

### 11.1 Assigned items

One row per non-terminal item owned by the Session, with Project icon, kind, title, phase,
condition, item-step count, and a link to the item. It appears immediately after assignment and
remains until completion, cancellation, or reassignment. The Agent to-do read returns these rows
as `assigned_items` as well as the direct to-dos; root guides require that read at turn boundaries,
so an assignment made during a working turn waits without terminal input and becomes the next
owned work.

### 11.2 Direct to-dos

The `+` control opens a modal and creates a quick to-do for the open Session. The person may attach
up to six raster reference images before creating it. Each image is normalized to PNG, stored as a
bounded child of the to-do, and returned as metadata; its bytes use the same opaque on-demand image
route as Board references. A row has text, reference-image metadata, creation order, state, and
three independent receipts:

- `sent_at`: the person pressed Send and terminal delivery succeeded (`✓`);
- `read_at`: the Session read its to-do API (`✓✓`, which supersedes the single mark);
- `completed_at`: the person or Session checked it complete.

A Session may read an unsent row, in which case it moves directly from no mark to `✓✓`; Send is
then disabled to avoid duplicate delivery. Send and Delete are person-only. Send hands the durable
PNG bytes to the existing terminal picture-delivery path, so Claude Code receives pasted images
and other assistants receive readable drop paths under the same fallback rules as the composer.
Images cannot be appended after an explicit Send or completion; an Agent pull that races the short
upload sequence sees the complete set on its next read. Delete removes the row and cascades its image bytes from
the active store as explicitly requested; its security/operation audit contains metadata, not the
deleted text or images. Agent access may only read and complete its own row.

The Console draws `✓✓` as an overlapping double-check and exposes localized accessible text for
unsent, sent, and read; color alone never carries the receipt state.

Direct to-dos are returned oldest first so an Agent can process them in order. The Agent may use
children for independent rows, but Clawdline does not automatically schedule or parallelize them.

### 11.3 Execution detail

Broker tasks, child deliveries, and landing obligations may be shown in a folded diagnostic group
or nested under an assigned item. They are not counted or styled as assigned Board items.

### 11.4 Pull does not mean wake

An unsent quick to-do is a pull queue. A Session that is completely idle has no turn in which to
poll it. Only the person's Send action provides immediate delivery/wake semantics. The guide asks
root Sessions to read their work at the beginning of a turn and before ending a substantial turn;
the daemon must not simulate Send in the background.

## 12. Console

The product has one authoritative Board. The retired Project Board is removed from navigation and
is not used as a second live source.

The Board provides:

- all-Projects and Project-scoped views;
- a Project-icon picker for creation and filtering; both the closed trigger and every Project row
  show the registered Project icon;
- an explicit icon-and-description list for Feature, Issue, Epic, Refactor, and Plan instead of a
  native kind dropdown;
- visible Edit and Delete controls on each current card; Edit changes title and description in a
  dismissible modal, while Delete asks for confirmation, removes the card and Session projection,
  and retains the cancellation audit instead of erasing why assigned work disappeared;
- Planning, Unassigned, Assigned, Implementing, Verifying, Merging, Deploying, and Recently Done
  areas;
- Feature/Issue cards with Project icon on every card;
- each Session responsibility shows explicit Implementation, Verification, Commit/Merge,
  Deployment, and Done milestones; completed milestones use a labelled green check, the current
  milestone is visually distinct, and recently completed responsibilities remain visible after
  their active assignment is released;
- an item detail view for description, documents, steps, execution, evidence, assignments, and
  immutable event history;
- card and direct-to-do thumbnails for durable reference images, with full-size viewing and
  person-only upload controls before the subject becomes terminal or delivered;
- an assignment dialog for new or existing Sessions;
- a separate proposal preview queue.

Creation stays in its modal while validation or transport fails, and the failure is shown inside
that modal rather than behind its backdrop. Escape, the close control, and a pointer press on the
backdrop close the modal; a press inside it does not. Buttons, Project rows, kind rows, cards, and
other clickable controls use the pointer cursor, while disabled controls retain a disabled cursor.

Presentation area is deterministic: planning kinds go to Planning; terminal items go to Recently
Done; an executable nonterminal item without an owner goes to Unassigned (while retaining a badge
for its last phase); every other item goes to its lifecycle area. A Project filter changes only
membership, never stored placement.

The whole-machine Now page may summarize v2 facts but owns no duplicate state. Project pages link
to the authoritative Board already filtered to that Project.

## 13. Capacity and concurrency

Every new bound is registered in `internal/domain/capacity` before implementation. At minimum the
design needs limits for open items, planning items, pending proposals, active assignments, item
steps, item documents, reference-image count and bytes, direct Session to-dos, and page sizes.
Reference images are limited to six per item or direct to-do, 5 MiB normalized per image, 15 MiB per subject, and
512 MiB across the store; an upload request is at most 18 MiB so its encrypted Cloud envelope also
fits the wire bound. Hitting a limit refuses the new row;
it does not evict a person's work.

Writes use the common receipt mechanism and optimistic item versions. A transaction reads the
current item, checks actor/owner/version/transition guards, writes the next state and event, and
records any external effect intent. Terminal, Git, and network I/O run after commit.

## 14. Destructive v1 cutover

The owner explicitly decided that old Board content has no value and may be permanently deleted.
There is no content migration and no content backup.

The cutover is an explicit local administrative operation, never an automatic schema upgrade:

1. replace Project-catalog dependencies on the retired Board source;
2. disable every v1 Board/session-to-do producer and reconciliation path;
3. dry-run the exact v1 table counts and legacy Board file targets;
4. require an exact confirmation over those targets;
5. delete old work, board, backlog, moves, proposals, Board-related decisions/digests, and
   dispatch-derived to-dos;
6. delete only legacy files positively identified as Board content, never an entire retired state
   directory;
7. retain transcripts, broker tasks, landing evidence, Git data, worktrees, settings, credentials,
   and Project/icon registries;
8. record a receipt containing targets, counts, time, and outcome, but no deleted content;
9. restart and prove no old task can reconstruct a v1 item or to-do;
10. enable the empty v2 writers and verify every v2 list begins at zero.

The retired source checkout is never modified. Old `work_id` values in retained task history are
historical strings only and are excluded by the v2 epoch/schema; they cannot bind to a new item.

## 15. Delivery sequence

1. Domain state machine, actor rules, and red acceptance controls.
2. v2 tables, transactions, receipts, events, and capacity rows.
3. person-only item/project/kind CRUD and the empty Board UI.
4. assignment to existing/new Sessions and the broker responsibility change.
5. owning-Agent metadata, document, step, phase, and evidence commands.
6. Session assigned-item projection and direct to-do `+`, Send/Delete/check, `✓`/`✓✓`.
7. explicit proposal preview/accept/reject.
8. Now/Project links, Cloud route coverage, and accessibility/e2e verification.
9. destructive v1 cutover, zero-state verification, and release.

No later step may reintroduce an automatic item-creation path to make an earlier step easier.
