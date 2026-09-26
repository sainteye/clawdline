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

1. **A work item is created by a person, or by a Session relaying a person's explicit message
   through that message's run.** *(Amended 2026-09-25 by the owner; it read "Only a person creates a
   work item" until then.)* Device-authenticated UI/API actions create items as the person. The
   one machine-credential path is `POST /v1/work/v2/agent/items`, and it needs the run of a message
   the person sent that same Session through this daemon (§7.1). Task secrets never create one, and
   a person typing straight into a terminal leaves no run, so that case stays a proposal (§10).
2. **Only a person assigns, reassigns, unassigns, deletes/cancels, completes by hand, or reopens an
   item.** An Agent may not
   appoint itself or another Session — except that an item created under §7.1 arrives assigned to
   the Session that relayed the person's message, and an item claimed under §7.2 is assigned to
   the Session the person's message told to take it, because that message is the person's
   assignment. A person's completion (`POST /v1/work/v2/items/<id>/complete`, event
   `item.completed`) needs no evidence and is not refused by open steps, whose count it records; no
   Agent can retract it — only a person reopens it.
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

On a successful assignment, an executable item with no existing steps treats two or more
top-level Markdown list rows in its description as explicit subitems and atomically seeds one step
per row. A single list row is left as prose, nested rows are not promoted, and reassignment never
duplicates existing steps. The description remains the authoritative full text; an overlong step
label is shortened only for the bounded TODO presentation.

Steps do not create Board items, do not assign Sessions, and do not automatically advance the
item's lifecycle. The owning Agent must explicitly complete every step before `done` is accepted;
other phase transitions do not silently complete them. A completed item retains its steps as
history.

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

Since 2026-09-26 an image's bytes are a file, not a row's BLOB: `<id>.png` or `<id>.jpg` (the stored
media type) under `reference-images/` in the daemon's state directory, written through a temporary
name, synced and renamed before the row that names it commits. The row, in `work_v2_images` or
`session_direct_todo_images`, keeps the metadata above and the file's sha256; every read of the bytes
proves the file against it, and a file that is gone or differs is refused 410 `image_file_missing` or
`image_file_mismatch`, never served empty. Deleting an image, or the to-do its rows cascade from,
removes the file after the commit, and a sweep at Open and every six hours removes files no row
names, only under those names and only once an hour old (limits N48). A store opened with the bytes
still in SQLite moves them out in bounded steps, each file verified before its bytes are cleared,
then rebuilds both tables without the `data` column and runs `VACUUM`.

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
| `done` | owning Agent | deployment evidence is valid, or deployment is explicitly not required; every item step is complete |
| `cancelled` | person | cancellation reason; assignment released |

The `done` transition records the completing owner, releases the active assignment, and removes
the item from the Session's open-responsibility projection in the same transaction. Its assignment
history and completion evidence remain on the item and in that Session's recent-completion
projection. A cancellation does the same release under the person's authority but is not presented
as successful completion.

An item assigned directly to an existing Session has no broker child task and therefore no child
landing record. A new Session opened as a Root Assignment likewise has no child landing record,
but its active assignment carries the resolved Root Assignment id. In either case, its owning Agent
may name an exact commit, local target branch, and remote. The daemon resolves all three through Git
and records a landing receipt only when the commit is on both the local target and
`refs/remotes/<remote>/<target>`. A new-Session assignment without its resolved Root Assignment id,
caller text, a successful build by itself, or an unpushed local commit never earns the landing check.

The owning-Agent phase request carries the claim as structured input, not prose:

```json
{"next":"deploying","landing":{"commit":"<full commit>","target":"main","remote":"origin"}}
```

The stored event replaces `<full commit>` and both target spellings with Git's resolved object ids.

Work can land outside the item's own Project: a backend item whose change turned out to be a
frontend commit. The landing then carries `"project": "<place id>"`, and the daemon looks for the
commit in that catalog Project's repository instead, refusing an id the catalog does not hold
(`landing_project_not_found`); the receipt carries the `repository` it was verified in. A caller
still cannot name an arbitrary path, only a Project this machine already lists.

The request is `POST /v1/work/v2/agent/items/<id>/phase`, and `clawdline item phase <item id> <next>`
sends it with the version it reads first. The owner's edit route refuses a `phase` field by name
(`phase_not_editable`) and points at this route, and every brief an owner receives — the courtesy
brief typed into an existing Session and a Root Assignment's acceptance — names the command. Before
2026-09-26 neither the guide nor any brief did, and a Session that had deployed its work left the
item in `assigned`.

### 6.1 Rework and failure

- A failed verification returns to `implementing` with the failed check preserved.
- A merge problem returns to `implementing` or `verifying`; prior landing evidence remains history.
- A deployment failure stays in `deploying` with a blocker. It does not become done.
- A person may reopen a completed or cancelled item. When a person's follow-up clearly says a
  Session's just-completed result is still unfinished, that same completing Session may retract
  only its own `done` claim with a concrete reason. The correction restores that Session as owner
  in `implementing`; it cannot reverse a cancellation or take another Session's completion.
  Either reopening starts a new execution cycle and never rewrites the earlier one.

### 6.2 Conditions are not phases

`blocked`, `waiting_user`, `owner_required`, `owner_offline`, and `evidence_unknown` are conditions
layered on the current phase. An item may therefore say “verifying · waiting for you” rather than
losing the stage at which it is blocked.

Only the owning Agent may set or clear an Agent condition. The broker derives `owner_offline` and
`evidence_unknown` from typed readings without moving the main phase.

`waiting_user` always names the requested action in `user_action`; setting the condition without
that text is refused. The field is owner-written, is capped at 8 KiB, and is cleared atomically
when the condition clears or the item changes phase, owner, or terminal state. Board cards and the
assigned-item detail show the request as a first-class callout rather than asking the person to
infer it from the description.

### 6.3 Deployment policy

`deployment_policy` is `required`, `not_required`, or `agent_decides`; new executable items default
to `agent_decides`.

When the policy is `agent_decides`, skipping deployment requires an Agent-authored
`not_applicable` decision with a concrete reason. `done` is refused if neither deploy evidence nor
that decision exists. A person may change the policy; the Agent may not.

## 7. Authority

| Operation | Person/device | Owning Agent | Other Agent | Broker/rule |
| --- | ---: | ---: | ---: | ---: |
| Create item | yes | yes, on the person's explicit message relayed by its run (§7.1); arrives assigned to itself | no | no |
| Assign/reassign/unassign/delete (cancel) | yes | no | no | no |
| Complete by hand (to `done` without evidence) | yes | no | no | no |
| Reopen terminal work | yes | own just-completed `done` only, never a person's completion | no | no |
| Change Project/kind/deployment policy | yes | no | no | no |
| Edit title/description | yes | yes | no | no |
| Add/edit documents and steps | yes | yes | no | no |
| Add/delete reference images | yes | no | no | no |
| Advance/rewind execution phase | no | yes | no | validates only |
| Create proposal | no | yes | yes, attributed to its root | no |
| Accept/reject/edit proposal | yes | no | no | no |
| Create quick Session to-do | yes | yes, for itself, on the person's explicit request | no | no |
| Send/Delete quick Session to-do | yes | no | no | no |
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

The owning Agent's own to-dos sit behind the same cooperative boundary. The rule that it writes
them only when the person explicitly asked is carried by the guide, not enforced by the daemon; what
the daemon enforces is that the conversation is one lowercase UUID naming exactly one live,
non-child Session, and it records that conversation id as the row's `created_by`. A Session that
forges another's conversation id is outside what this boundary claims to stop.

### 7.1 A Session creating an item on the person's message

Decided by the owner on 2026-09-25. When the person tells a Session, in a message sent through
Clawdline, to create a Board item, the Session creates it directly instead of filing a proposal the
person must then accept and assign by hand.

The proof is the relay mechanism that already exists (`internal/domain/work/runs.go`): a run is
issued only when a person sends a Session a message through this daemon, and
`GET /v1/orchestrator/sessions/<conversation>/run` answers the latest one. The Session names it:

```
POST /v1/work/v2/agent/items     (machine auth, Idempotency-Key required)
{"session_id": "<conversation id>", "via": {"run": "<run id>"}, "project_id": "<place id>",
 "kind": "…", "title": "…", "description": "…", "deployment_policy"?: "…", "steps"?: ["…"]}
```

Each check refuses by a typed code and writes nothing: the run exists (`run_unknown`, also for no
run named), is within the one-day relay window (`run_expired`), and was said to this Session
(`run_other_session`); the conversation is a live, non-child Session (`session_not_found`,
`child_session`); the Project is in the catalog (`project_not_found`); an executable item is in the
Project the Session works in (`project_mismatch`); the usual title and description rules; at most
128 explicit steps (`too_many_steps`), none on a planning kind (`planning_has_no_steps`); and one
run backs at most five created items, open or closed (`run_items_exhausted`, capacity row
`run.created_items`).

One transaction writes the item with `created_by = user_via_session:<run>` and an `item.created`
event carrying `via_run` and `session_id`. For Feature and Issue it also writes an active
`existing_session` assignment to that Session, moves the item to `assigned`, and writes its steps:
the explicit `steps` in order, or — when there are none — the description's two or more top-level
list rows exactly as a person's assignment seeds them. Explicit steps replace that seeding, so no
step is written twice. Nothing is typed into the Session's terminal: it asked. Epic, Refactor and
Plan are created unassigned in Planning, as a person's would be.

The run now keeps an excerpt of the message (280 characters, whitespace folded, cut on a character
boundary with an ellipsis; runs issued earlier have none), and the item keeps its provenance in
`created_via`: the run, the conversation, when the message was said, and that excerpt. The item
wire carries it as `created_via` and the Console draws "Created by the Session from your message at
HH:MM" on the card, quoting the excerpt. A person's own item carries none.

The boundary is the same cooperative one as §7's: the machine credential proves "a Session on this
machine", and `run_other_session` stops a Session using another Session's run by mistake, not by
intent. That a Session creates items only when the person's message explicitly asks is carried by
the guide, like its own to-dos. The proposal path (§10) stays the fallback for everything without a
run.

### 7.2 A Session claiming an item on the person's message

The same relay, for an item already on the Board: the person tells a Session, in a message through
Clawdline, to take a specific item. The person's own assign route stays the person's
(`session_cannot_create_item` for any machine caller); the Session calls its own:

```
POST /v1/work/v2/agent/items/<id>/claim     (machine auth, Idempotency-Key required)
{"expected_version": N, "session_id": "<conversation id>", "via": {"run": "<run id>"}}
```

There is no field naming a Session or terminal: the item goes to the Session the run was said to,
and only to it. The run and Session are checked exactly as §7.1 checks them (`run_unknown`,
`run_expired`, `run_other_session`, `session_not_found`, `child_session`), then the item: it exists
(`work_not_found`), is in the Project the Session works in (`project_mismatch`, the person's
`existing_session` check), is not a planning kind (`planning_not_assignable`) or finished
(`item_terminal`), has not changed (`version_conflict`), and has no Session and none being opened
for it (`item_assigned` — a person may move an item between Sessions, a Session may only take one
nobody holds). One run backs at most five claims, active or released (`run_claims_exhausted`,
capacity row `run.claimed_items`, mirroring `run.created_items`). Every refusal writes nothing.

Underneath it is the person's `existing_session` Assign — one transaction writing the active
assignment, the owner, `assigned`, the description-seeded steps and the `item.assigned` event — with
two differences: the actor is `user_via_session:<run>`, and the assignment keeps the run's provenance
in `claimed_via` (the event carries `via_run`, `claimed` and the excerpt). Nothing is typed into the
terminal. The item wire carries it as `claimed_via`, on the item for its active assignment and on
that assignment, and the Console draws "Claimed by the Session from your message at HH:MM" on the
card, quoting the excerpt. `clawdline item claim <item id>` is the command; the guide says a Session
runs it only when the person's message names the item.

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

An open assigned item exposes **Remind Session** on both its Board card and its Session detail.
This is a person-only, idempotent action against the item's current active assignment. It verifies
that the recorded terminal still belongs to the recorded conversation before typing, then sends a
fresh reminder even when the Session is working. The reminder points the Agent back to the durable
item, where the latest description, reference images, documents, and steps live; it does not create
a second assignment or change the item's version. Gone, unreadable, reused, and busy terminals are
distinct refusals, and terminal items or unassigned items cannot be reminded.

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

Successful activation is also the claim boundary for structured subitems: when the item has no
steps and its description has at least two top-level Markdown list rows, activation seeds those
steps in the same transaction as ownership. A failed opening seeds nothing.

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

A proposal is also the fallback when a Session has no run to create an item under (§7.1): the
person typed straight into the terminal, so the Session proposes and tells them to accept it here.

It does not expire into acceptance, does not create a work item through a rule, and is never mixed
with the Board count. The person can preview and edit it before accepting. Acceptance creates one
new item and links the proposal to it in one idempotent transaction. Rejection keeps provenance
without creating work.

## 11. Session to-do panel

The panel has three explicitly labelled groups.

### 11.1 Assigned items

One row per non-terminal item owned by the Session, with Project icon, kind, title, phase,
condition, item-step count, and each step's completion receipt. A control opens the item's description,
requested user action, progress, deployment policy, and reference images in a modal. It appears
immediately after assignment and remains until completion, cancellation, or reassignment. The Agent
to-do read returns these rows
as `assigned_items` as well as the direct to-dos; root guides require that read at turn boundaries,
so an assignment made during a working turn waits without terminal input and becomes the next
owned work.

### 11.2 Direct to-dos

The `+` control opens a modal and creates a quick to-do for the open Session. The person may attach
up to six raster reference images before creating it. Each image is normalized to PNG, stored as a
bounded child of the to-do, and returned as metadata; its bytes use the same opaque on-demand image
route as Board references. A row has text, reference-image metadata, creation order, state, and
three independent receipts:

- `sent_at`: the person most recently pressed Send and terminal delivery succeeded (`✓`);
- `read_at`: the Session read its to-do API after that delivery (`✓✓`, which supersedes the single mark);
- `completed_at`: the person or Session checked it complete.

A Session may read an unsent row, in which case it moves directly from no mark to `✓✓`. This is a
queue-synchronization receipt, not a claim that implementation started: a turn-boundary poll can
observe the row while the Session is still finishing earlier work. If the row remains open, the
person may press Send again. A successful reminder replaces `sent_at` and clears the earlier
`read_at`, returning the row to `✓` until the Session reads its queue again. A sent row that has not
yet been read cannot be doubled. Send and Delete are person-only. Send hands the durable PNG bytes
to the existing terminal picture-delivery path, so Claude Code receives pasted images and other
assistants receive readable drop paths under the same fallback rules as the composer.
Images cannot be appended after an explicit Send or completion; an Agent pull that races the short
upload sequence sees the complete set on its next read. Delete removes the row, cascades its image rows, and removes their files
once that commits; its security/operation audit contains metadata, not the
deleted text or images. Agent access may read and complete its own rows, and add rows to its own
list as below; it can never send or delete one.

**Send names the row.** Send — the first one and every reminder — types the row's text unchanged
(trailing line breaks dropped), a blank line, and one last line:
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)`. Pictures are unchanged. The
text is built in one place, `app.DirectTodoSendText`, and the phone's Send reaches it too: the
Cloud `work.v2.todo-action` operation is carried to the same route. Measured on 2026-09-25: before
this line, a Session received an ordinary message with no id, did the work, deployed and reported
its turn, and the row stayed open because nothing told it which row it had finished.

**The turn receipt reminds.** `POST /v1/orchestrator/sessions/<terminal>/complete` (`clawdline
session report`) records the receipt first and then also answers `open_todos`: the Session's direct
to-dos with `sent_at` or `read_at` set and no `completed_at`, oldest first, at most 20
(`session.report_open_todo_rows`; `open_todos_truncated` says there were more), each
`{id, text, sent_at, read_at}` with the text folded to one line and cut at 120 characters
(`session.report_open_todo_characters`). Session-created rows are included, since their `read_at`
is set at creation. The read does not mark anything read. A turn may legitimately end with to-dos
open, so the receipt is never refused for them; when they cannot be read the answer carries
`open_todos: []` with `open_todos_unknown: true`, because unknown is not zero. The command prints
the rows on stderr after the daemon's JSON with `clawdline todo done <id>` as the way to complete
each, and its exit status is unchanged.

**Session-created rows.** When the person explicitly asks a Session to track its work as Clawdline
to-dos, the Session adds them itself with
`POST /v1/work/v2/agent/session-todos/<conversation id>` (`clawdline todo add`), body
`{"todos": [{"text": "…"}, …]}`, Idempotency-Key required. One call carries 1–20 rows
(`session.todo_batch_rows`), each under the same trim, non-empty and 8 KiB rules as a person's row,
inside the 96 KiB work-system request body. The batch is one transaction: every row is written or
none is, and a batch that would take the Session past its 500 open rows is refused whole
(`direct_todos_full`). A malformed conversation id, one that names no live Session, one that is
ambiguous, a registry that cannot be read, and a live Clawdline child's Session are all refused
before anything is written (`conversation_id_malformed`, `session_not_found`,
`conversation_ambiguous`, `registry_stale`, `child_session`).

Such a row records the Session's conversation id as `created_by`, and its `read_at` equals its
`created_at`: the Session wrote it, so there is no delivery for it to observe. Rows are read back in
the order given; rows created in the same second are ordered by insertion (`rowid`), not by their
random ids. Every direct-to-do row carries `created_by` on both the person's and the Agent's read.
The Console shows a row whose `created_by` equals the Session's own conversation id with a
localized "Added by Session" label (with accessible text) in place of the sent/read receipt, since
the person never sent it; a completed one still shows its completion. Complete, Send and Delete
controls are unchanged, and only the person can send or delete such a row. Session-created rows
never appear on the Board; a Board item comes only through an Agent proposal (§10), which may cite
the to-do as its `source_todo_id`.

The Console draws `✓✓` as an overlapping double-check and exposes localized accessible text for
unsent, sent, synchronized-but-open, and completed; color alone never carries the receipt state.
Completed rows remain in a recent-completion group with a green check until the person explicitly
deletes them. The open count excludes those retained confirmations.

Open direct to-dos are returned oldest first so an Agent can process them in order. Person reads
place all open rows first and then the newest completed rows, so the bounded view cannot hide old
unfinished work behind completion history. The Agent may use children for independent rows, but
Clawdline does not automatically schedule or parallelize them.

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

No other page keeps a machine-wide summary of work (the Now page was retired on 2026-09-26). Project pages link
to the authoritative Board already filtered to that Project.

## 13. Capacity and concurrency

Every new bound is registered in `internal/domain/capacity` before implementation. At minimum the
design needs limits for open items, planning items, pending proposals, active assignments, item
steps, item documents, reference-image count and bytes, direct Session to-dos, and page sizes.
Reference images are limited to six per item or direct to-do, 5 MiB normalized per image, 15 MiB per subject, and
512 MiB across the store (the rows' byte counts, which are the files' sizes); an upload request is at most 18 MiB so its encrypted Cloud envelope also
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
