# Work system v2

Status: **implemented locally; release verification and live-data cutover pending**. The owner
approved the defaults in this document on 2026-09-22. The v2 store, authority boundaries,
lifecycle API, Board Console, Session projection, direct to-dos, proposals, and guarded v1 reset
are present on the implementation branch. The person's running daemon has not been changed and
its v1 rows have not been deleted.

This page is normative for the replacement. Where the earlier board design, work-system design,
or broker projection disagrees with it, this page wins for v2. The acceptance contract is
[`work-system-v2-acceptance.md`](work-system-v2-acceptance.md).

Agent requests for a person's attention on a Session use the separate
[`human-interventions.md`](human-interventions.md) contract. They do not change
direct Session to-dos or formal Board decisions.

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
   *(Amended 2026-09-27:)* the owner Session of an Epic past its plan gate may also create Feature
   and Issue items under that Epic (§6.5): the person's assignment of the Epic is the authority, so
   no run is needed, and nothing else may be created that way.
2. **Only a person assigns, reassigns, unassigns, deletes/cancels, completes by hand, or reopens an
   item.** An Agent may not
   appoint itself or another Session — except that an item created under §7.1 arrives assigned to
   the Session that relayed the person's message, and an item claimed under §7.2 is assigned to
   the Session the person's message told to take it, because that message is the person's
   assignment — and *(amended 2026-09-27)* the owner Session of an Epic assigns and reassigns that
   Epic's own open child items (§6.5), because the person assigned it the Epic. A person's completion (`POST /v1/work/v2/items/<id>/complete`, event
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
| `epic` | Unassigned | yes, behind a plan-and-review gate (§6.4) | A large body of work its owner plans, has reviewed, and then executes in steps |
| `refactor` | Planning | no | A planned structural improvement, not yet execution work |
| `plan` | Planning | no | A saved plan or investigation direction |

Planning items (Refactor and Plan) are retained and editable but do not acquire an owner or enter
the execution lifecycle in v2.0. Epic was a planning kind until 2026-09-27; it became executable
then, and Epics already in a store simply appear in Unassigned — nothing was migrated. An Epic's
owner creates linked Feature/Issue items under it (§6.5); nothing silently mutates a planning item
into executable work.

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
- the persona a `new_session` assignment opened the session as (`persona`, empty for none;
  docs/personas.md);
- `assigning`, `active`, `released`, or `failed` state;
- human actor and timestamps;
- failure/refusal evidence.

The current owner is the one active assignment. A failed attempt is history, not an owner.

### 5.2 Documents

An item may have ordered document references with a closed role: `spec`, `design`, `test`,
`deploy`, `completion_report`, `other`, and — on an Epic or Feature (`document_role_not_applicable`
elsewhere) — `plan` and `plan_review`, the record §6.4's gate reads. A document may carry Markdown content or a repository-relative path/URL with
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
| `implementing` | owning Agent | owner identity matches; captured planning gate, when on, has plan and independent review |
| `verifying` | owning Agent | when captured verification is on, a clean committed same-Project candidate is frozen and a checker round is queued |
| `merging` | owning Agent | when captured verification is on, a live PASS or reasoned exact-candidate override; otherwise ordinary verification evidence |
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

### 6.4 Captured planning for Epic and Feature

The planning setting defaults on, but the first successful assignment of an execution cycle
captures it with the verification setting. A captured planning-on Epic or Feature cannot go
`assigned → implementing` until it holds acceptance criteria, a `plan` document, and a
`plan_review` document written after the latest `plan`. The owner writes the plan (`clawdline item doc <id> --role plan`),
dispatches a read-only child with `--kind plan_review --work-id <id>`, and records that child's
review as a `plan_review` whose `reference` is the child's task id and whose body is the review's
substance for the person — what it found and what the plan changed in response. A plan rewritten
after a review needs a fresh review, up to two required reviews for Epic or one for Feature: a
plan rewritten after the ceiling goes on without another forced review. Issue is always exempt.
Planning off bypasses the forced plan and review even for Epic.

The phase route refuses a planning-on transition when the plan or review is absent. The document route accepts
a `plan_review` only when its task is a real review of this plan, read from the broker's task
record in the same transaction as the item's owner: the task exists
(`plan_review_task_unknown`), was dispatched by the item's owning Session
(`plan_review_task_not_owned`), is on this item's line when it names one
(`plan_review_task_other_item`), has kind `plan_review` (`plan_review_task_wrong_kind`), finished
with `success` (`plan_review_task_unfinished`), and was dispatched no earlier than the latest plan
(`plan_review_task_stale`), each a `422`; a review with no plan is `epic_plan_required`. A dispatch
may name a v2 Board item as its `work_id`: the broker accepts one in the dispatch's Project that is
not terminal, and leaves the item's phase to its owner.

Documents are ordered for the gate by their creation second and then by insertion, so a plan and a
review written in the same second keep the order they were written in. Owner briefs and Root
Assignments include the exact acceptance and captured mode; the global switch cannot rewrite a
live cycle.

### 6.4a Independent verification, escalation, and recovery

`verify_gate` defaults off. A successful first assignment captures both gate values; a failed
new-Session open captures neither. Reassignment preserves the pair. Person reopen clears the old
snapshot for a new assignment; retraction by the just-completing owner copies the pair into its
correction cycle but invalidates the old verification authorization. Migrated in-flight Epics keep
planning on and verification off; migrated Features and Issues keep both off. Terminal history is
not retroactively gated. The four modes are independent: both on, planning only, verification only,
or both off. Planning off explicitly bypasses Epic planning.

Acceptance is one bounded Markdown field with version and SHA-256 digest. A person may create or
edit it, but the Board does not ask them to supply it. Assignment captures the gates even when
acceptance is empty. The owning Agent then writes the first criteria with
`clawdline item acceptance <id> --body-file <file>` before a planning-on Epic or Feature starts
implementation, or before verify-on work enters verification. An Epic owner may also set it for
a child or revise it through a reasoned third-FAIL decision. After the first nonempty criteria,
the maker cannot revise it through the ordinary edit route. The owning live Root may relay a person's explicit Clawdline message with
`clawdline item acceptance-revise <id> --run <run> --expected-version <item version> --body-file <file>`.
The message excerpt must explicitly request an acceptance change; negations, questions without
a direct request, and discussion alone do not authorize the route. It must name the item by ID
or title when the Root owns more than one open item; with exactly one open item, its conversation
context identifies that item unless the message names a different item ID. The run must be newer
than the current acceptance version and within the run's one-day relay window. The
server records the run, Session, time, and excerpt in the item event and response. The full file
replaces the acceptance Markdown; use the same key and version to retry an uncertain write.
The person may still edit directly. An authorized edit before merging stales the current round and every old
PASS/override, returning a verifying item to implementing. Merging, deploying, and done lock it.
The checker receives its exact text, version, digest, cycle, and immutable candidate receipt.
The Board's planning-and-verification panel appears only for an Epic after criteria have been
written. Person create/edit forms omit the criteria field; a non-Epic's pending verification
escalation remains a separate actionable decision rather than an empty gate panel.

With verification on, the maker's `clawdline item phase <id> verifying` sends its registered
same-Project worktree, branch, and full committed HEAD. The daemon requires a clean tracked tree
and a strict descendant of the cycle base, then durably queues a detached read-only Codex checker.
Issue uses Code Reviewer; Epic final end-to-end verification uses Reality Checker only after all
children are terminal; Feature uses Evidence Collector when it has a reference image or design
document, otherwise Reality Checker. A result has `PASS`, `FAIL`, or `NEEDS_WORK` with bounded
per-claim evidence; an unverified claim explains why and is never PASS. Missing, malformed, stale,
or mismatched results cannot authorize merging. Technical failure gets one bounded retry before
escalation. Repeated FAIL returns to implementing and the third consecutive FAIL escalates; a live
parent Epic owner decides first, otherwise the person. Technical escalation is separate. Direction,
acceptance revision, reassignment, repair-retry (technical only), reasoned override, and cancellation
are closed actions; only override grants exact-candidate authority without checker PASS. The Board
labels AI/person and technical overrides explicitly.

The Board list carries only the latest round, escalation, authorization, and four fixed-size
aggregates (rounds, FAILs, findings, overrides). Single-item reads add at most ten recent rounds
and their bounded evidence. Capacity refusal does not evict evidence. The person can export eligible
closed detail with `GET /v1/work/v2/items/<id>/gate-export`, save the exact UTF-8 document and
verify its manifest digest, then confirm `POST /v1/work/v2/items/<id>/gate-purge` with that digest
and item version. Purge preserves active/latest protected detail, fixed-size aggregates, and the
audit event. Cloud carries these same person routes; the parent-Epic Agent decision remains on its
machine-authenticated Agent route. An export version or digest conflict requires a fresh export.

### 6.4b Conditional UX and product review for an Epic

An Epic plan classifies whether its proposed or implemented result changes a human-facing
interface, user journey, or product policy. A backend-only Epic records why it has no such impact
and stops there. This keeps a second design ceremony out of work where it cannot improve the
outcome.

When any of those person-facing effects exist, the Epic owner must, before merging, dispatch at
least one independent read-only specialist against the integrated candidate. Layout, interaction,
and end-to-end product flow use the UX Architect by default:

```
clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
```

The brief requires desktop and smallest-supported-mobile evidence, keyboard and screen-reader
behavior, dead ends, product fit, severity, and a concrete recommendation. When evidence is
unavailable the reviewer must **mark unverified and say why**, never infer a pass. `product-manager`
may replace `ux-architect` when policy and scope rather than layout is the dominant risk;
`ui-finish-gate` is an additional pre-ship visual pass when the finish itself is material. Every
blocking finding is resolved before merging. The Epic's verification evidence or completion report
records the review task id, verdict, and disposition of its findings.

The owner records the scope covered by that review. One integrated review normally covers the
Epic: a small correction to copy, spacing, tests, or a finding within the reviewed scope is
checked and closed by the owner without dispatching the same reviewer again. Brand, security,
and other specialists are added only for a material risk in their own area, not as a roster.
Redispatch only if a later change materially changes the user journey, product policy, brand
direction, security boundary, or another risk outside the recorded review scope. The owner names
that change and requests one focused review of the new boundary, not a repeat of every earlier
specialist. This does not relax the captured plan gate or exact-candidate verification gate.

That visual dispatch matches both the worker surface and `permission_mode` to its tools. Codex CLI
does not provide the ChatGPT desktop app's built-in `@Browser`, and `full` selects
`--ask-for-approval never`; neither is blanket Browser or Computer Use access. Every dispatch lists
the tools its acceptance needs. A UI reviewer runs on a surface that actually provides
Browser/Computer Use, or with a named acceptance-equivalent local harness such as Playwright or
Chrome CDP, and uses `ask` when an app, origin or GUI approval may be needed. At task start it
exercises that tool. If it is absent or authorization cannot be obtained, the reviewer reports the
launch defect immediately and the Epic owner restores access or redispatches. A tool-dependent
visual gate does not finish as unverified because the root selected an incompatible worker.

This is a conditional owner process rule, not another daemon authorization token. It does not
replace the plan review, the verification checker's PASS, or the Epic's final end-to-end check.

### 6.5 An Epic's owner breaks it into child items and hands them out

*(Added 2026-09-27 by the owner: "the Session that holds an Epic has the power to create Feature and
Issue items and assign them to other Sessions".)* An Epic is large and its owner coordinates it; a
plan when the captured gate requires one (§6.4) often says that parts are better done by other
Sessions. Until this change the owner could not act on that: a Session created an item only on a
person's message (§7.1), the item was always assigned to the creating Session, only a person
assigned items to Sessions, and a Root opened for an item was told "Do not create Board items". So
the person assigning an Epic to a Session is now the authority for that Session to break it up —
and only it:

- `POST /v1/work/v2/agent/items/<epic id>/children` (`clawdline item child`) creates a Feature or
  Issue under the Epic, and `POST /v1/work/v2/agent/items/<child id>/assign` (`clawdline item
  assign`) (re)assigns an open child. Both need the Idempotency-Key and the machine credential, and
  the caller's `session_id` must be the Epic's active owner (`not_epic_owner`).
- The parent is an Epic (`parent_not_epic`), not terminal (`item_terminal`), and in phase
  `implementing` or later (`epic_not_planned`). If its captured planning gate is on, the plan
  and review guard was passed; if off, the Epic intentionally bypassed it.
- A child is a `feature` or an `issue` (`child_kind_not_allowed`): an Epic inside an Epic, or a
  Planning kind, is not a piece another Session can own and finish.
- One Epic holds at most 32 children, open or closed (`epic_children_full`, capacity row
  `epic.child_items`, limits N51): an Epic broken into more pieces than that is two Epics, and a
  Session looping on the route is stopped.
- The child is in the Epic's Project, records `parent_id` (the Epic; immutable, on the item wire as
  `parent_id`), `created_by = epic_owner:<session id>`, and `created_via = {"session_id", "at",
  "epic_id"}` with no run, so its card can say the Epic's owner Session created it. Its steps are the
  explicit `steps`, or its description's list when it is assigned, as for any item. The Epic gains
  an `epic.child_created` event.
- Assignment reuses the person's assignment path — an existing Session by terminal id, which must
  work in the Project, gets the typed brief and the assigned-item projection; a new Session gets a
  Root Assignment — with the owner check inside the assignment's transaction and the actor recorded
  as `epic_owner:<session id>`, a Session, never a person. Assigning a child to the Epic's owner
  itself is allowed. The child's owner is told which Epic it belongs to.

  For a new Session opened by the Epic owner, the Session snapshot projects an `epic_parent` with
  the Epic id and the assigning owner's conversation id from that assignment's recorded actor.
  The console places this independent Root under the owner's row, and keeps broker children under
  that Root. The source is the assignment made at the time, so reassigning the Epic does not move an
  existing Root to a different parent. If the owner is absent or filtered out, the Root remains a
  top-level visible row. This relationship is presentation ancestry: it does not grant child-task
  credentials, transfer landing duties, or change the Root's independent closeability.
- Creation and assignment are two writes, in that order. When the assignment fails the child
  **stays, unassigned** (a failed new-Session attempt is recorded as `assignment_failed`, as a
  person's would be), and the `201` answer carries `assigned: false` and `assignment_error: {code,
  message}` rather than pretending nothing happened. The idempotency receipt is filed only after
  both are known, so a replay answers the same and creates nothing more.
- An existing child moved by `…/assign` must have a parent Epic the caller owns; an item with no
  parent is refused `not_epic_child` and stays the person's to assign.
- The owner stays responsible for the Epic: the owning Agent's move of an Epic to `done` is refused
  `409 epic_children_open`, naming how many children are open, until every child is done or
  cancelled. A person's completion by hand (§2 invariant 2) is not gated: the person may close the
  whole regardless.

After integrating each child, the Epic owner immediately rereads the child and its steps. The
child's owning Session completes remaining steps and moves it beyond `merging`: it provides the
exact landing commit already on the local target and `origin/main`, advances to `deploying`, then
records deployment evidence or a no-deployment reason permitted by that child's policy and moves
to `done`. An Epic owner who also owns the child can perform these writes; otherwise the owner
follows up with or reassigns the child owner through the authorized path. `not_item_owner`
prevents signing another Session's steps or phase. The Epic owner ACKs any broker completion
notice, inspects the landing receipt and worktree inventory, and prunes only proven landed-identical
or task-temporary residue; unlanded, mixed, and unknown material stays preserved with a named next
owner. The parent Epic remains open until all children are `done` or `cancelled`.

Closing the Board items is separate from closing the independent Root Sessions that an Epic opened
with `--assign-new`. After a child's `done`, the Epic owner inventories those Roots through
`GET /v1/sessions`, matching `epic_parent.epic_id` and checking the exact Session's
`closeability`. The Root owner settles its own obligations and attests after a close audit;
`clawdline session report` is only a delivered-turn receipt. A supported Session close is attempted
only with verified identity and `closeability.state=safe`, then confirmed by a fresh inventory.
`blocked` is followed up with its named mover; `unknown` preserves the Session and records the
missing evidence and next owner. Neither a raw terminal close nor a forced archive substitutes
for closeability. The Epic's final coordination report names every Root's closure or its exact
blocker even when the Board items are already `done`.

The Epic's owner is told all of this where it is told the Epic procedure: the Root Assignment's
Constraints and acceptance and the assignment and reassignment briefs name `clawdline item child`
and `clawdline item assign`, the address book the terminal ids come from, and the responsibility
for the children. Every other owner's brief still says "Do not create Board items." Limits kept on
purpose: a child cannot have children of its own through this route (it is never an Epic); the
owner cannot cancel a child (a person can), and an Epic that is reassigned hands its children's
coordination to the new owner, whose `session_id` then passes the check.

## 7. Authority

| Operation | Person/device | Owning Agent | Other Agent | Broker/rule |
| --- | ---: | ---: | ---: | ---: |
| Create item | yes | yes, on the person's explicit message relayed by its run (§7.1); arrives assigned to itself. An Epic's owner: Feature/Issue children of that Epic, after its plan gate (§6.5) | no | no |
| Assign/reassign/unassign/delete (cancel) | yes | assign/reassign only, by an Epic's owner, of that Epic's open children (§6.5) | no | no |
| Complete by hand (to `done` without evidence) | yes | no | no | no |
| Reopen terminal work | yes | own just-completed `done` only, never a person's completion | no | no |
| Change Project/kind/deployment policy | yes | no | no | no |
| Edit title/description | yes | yes | no | no |
| Edit acceptance Markdown | yes before merging | may fill empty criteria once while assigned or implementing; an Epic owner may revise a child's criteria through a reasoned escalation decision | no | no |
| Decide a verification escalation | yes when routed to the person | designated live parent Epic owner through the Agent route only | no | no |
| Export and purge eligible verification detail | yes, in that order with matching digest and version | no | no | no |
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
 "kind": "…", "title": "…", "description": "…", "deployment_policy"?: "…", "steps"?: ["…"],
 "assign"?: {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}
```

Each check refuses by a typed code and writes nothing: the run exists (`run_unknown`, also for no
run named), is within the one-day relay window (`run_expired`), and was said to this Session
(`run_other_session`); the conversation is a live, non-child Session (`session_not_found`,
`child_session`); the Project is in the catalog (`project_not_found`); an executable item is in the
Project the Session works in (`project_mismatch`, except the registered machine steward's delegation path); the usual title and description rules; at most
128 explicit steps (`too_many_steps`), none on a planning kind, Refactor or Plan (`planning_has_no_steps`); and one
run backs at most five created items, open or closed (`run_items_exhausted`, capacity row
`run.created_items`).

One transaction writes the item with `created_by = user_via_session:<run>` and an `item.created`
event carrying `via_run` and `session_id`. For Feature, Issue and Epic it also writes an active
`existing_session` assignment to that Session, moves the item to `assigned`, and writes its steps:
the explicit `steps` in order, or — when there are none — the description's two or more top-level
list rows exactly as a person's assignment seeds them. Explicit steps replace that seeding, so no
step is written twice. Nothing is typed into the Session's terminal: it asked. Refactor and Plan
are created unassigned in Planning, as a person's would be.

The registered Clawdfather is the narrow exception for executable items: it works outside every
Project and never owns or edits Project code. On a person's explicit Clawdline message it first
creates the target Project's item unassigned, then may delegate it to a Project Session with
`clawdline item add --project … --assign-new` or `--assign-terminal`. The daemon requires its
current role binding to be online and to name the exact creating Session (`coordinator_required`),
and rejects `assign` on ordinary Session item creation (`machine_delegation_required`). The
item and an initial receipt naming its ID commit in one transaction. Assignment then runs and
refines the receipt. The response separates `item_created` from `assignment_state`: `assigned` only
when a Project owner is recorded, `awaiting_user` when a new Session stopped at its first dialog,
`failed` with `assignment_error`, or `not_requested` when no assignment was requested. An
`awaiting_user` item needs the person to answer the new Session's first screen; an unrequested or
failed assignment needs the person to assign it from the Board. If the process stops or receipt
refinement fails after creation, a replay of the same Idempotency-Key returns the original item ID
with `pending`; inspect the Board before any new assignment. It cannot create a duplicate item.
Without an explicit message authorizing a new item,
Clawdfather files a proposal and waits for the person to accept it.

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
`existing_session` check), is not a planning kind, Refactor or Plan (`planning_not_assignable`) or finished
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

A new Session whose first screen is a dialog — Codex's "Update available" menu opened two Board
items' Sessions on 2026-09-27 — is not a failure. Nothing answers it: the question is the person's.
The Root Assignment becomes `awaiting_dialog`, the assignment stays `assigning` with the Root
Assignment and tab recorded on it, and the item carries `waiting_user` with a `user_action` saying
which Session to answer. The broker's beat watches the tab: once the person answers and a composer is
up, the briefing line is typed and the assignment is activated as in step 6; if the assistant leaves
the tab or the tab is closed, it fails as above and the question is taken back. There is no clock —
closing the tab is how to give up. A second assignment of the item is refused
(`assignment_awaiting_dialog`) while one waits, so an answer cannot give it two owners. The daemon
finishes, when it starts, any such assignment whose Root was settled while it was down.

Codex is launched with `-c check_for_update_on_startup=false` for this one run, so its update menu
is not among those dialogs; a Codex the person opens themselves still offers the update.

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
proposing Session, one source item/quick-to-do, and version. Title, description, reason, and
suggested acceptance are all required and form a plain-language explanation for the person:
observable outcome, what changes, why it matters now, and what done looks like. Unexplained
acronyms, internal identifiers, paths, and implementation jargon are supporting detail, not the
proposal's main wording. Its states are `pending`, `accepted`, and `rejected`.

A proposal is also the fallback when a Session has no run to create an item under (§7.1): the
person typed straight into the terminal, so the Session proposes and tells them to accept it here.

It does not expire into acceptance, does not create a work item through a rule, and is never mixed
with the Board count. The compact row shows the title, source, and reason; its **Explain / 詳細說明**
control reveals what changes and the observable completion result. The person can preview and edit
it before accepting. Acceptance creates one new item and links the proposal to it in one idempotent
transaction. Rejection keeps provenance without creating work.

### 10.1 Questions that need a person's answer

A Session may ask a two-to-four-option decision only from an open Board item it owns. `work_id` is
required on `POST /v1/orchestrator/decisions`; the daemon derives Project from that item and refuses
a missing, unknown, closed, or differently owned source. The Board draws the open decision inside
that item's card, after the item's description, so its question, safe default and answer controls
never appear without the work that explains them. Store upgrade removes rows persisted before this
invariant when they have no `work_id`, and the database then rejects any new question without one;
linked questions and their answers remain unchanged.

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

The person's checkbox is reversible: pressing a completed row clears its completion receipt and
returns it to the unfinished list. Its send and read receipts remain intact because reopening the
work does not undo its earlier delivery.

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
