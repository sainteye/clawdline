# Work system v2 acceptance contract

Status: **approved release gate; implementation in progress**. Domain, store, app, capacity, and
Console build tests cover the initial implementation. The full failure-injection and browser
matrix below remains the release gate; the live v1 reset must not run before it is complete and
the owner has reviewed the exact dry-run counts.

This document is the release gate for [`work-system-v2.md`](work-system-v2.md). It describes tests
before choosing package or component boundaries. Each scenario must become an automated Go,
HTTP/contract, store, Console, or end-to-end test during implementation. A scenario is not complete
because a happy-path screenshot exists: each named negative control and failure injection must run.

## 1. Test rules

1. Every write is exercised twice with the same idempotency key and body; the second answer is a
   replay and the store/effect count remains one.
2. Every write is exercised with the same key and different body; it is refused without mutation.
3. Every versioned write is exercised against a stale version; it is refused and the newer bytes
   remain intact.
4. Every person/Agent boundary is tested with both credentials. A hidden button is not an
   authorization test.
5. Every external effect has failure injection before intent, after intent/before effect, and after
   effect/before answer where meaningful.
6. Unknown/unreadable input is distinct from absent input in store, identity, Project, Git, and
   deployment tests.
7. Tests use an empty temporary state directory and their own daemon/port. They never read or
   write the person's live state.
8. UI acceptance runs at desktop and narrow-phone widths with keyboard-only focus checks.

## 2. Creation and Project identity

### WS2-C01 — A person creates an executable item

Given a device with send capability and a catalog Project with an icon, when it creates a Feature
or Issue, then exactly one `created` item exists with the selected Project identity, title,
description, `agent_decides` deployment policy, version 1, and one creation event.

Negative controls:

- read-only device: `403`, no row/event;
- machine/orchestrator credential: `403 session_cannot_create_item`;
- task secret: `403`;
- unknown or unavailable Project identity: typed refusal, no fallback by path/name;
- arbitrary free-form Project path: rejected;
- unknown kind or missing title/description: rejected.

### WS2-C02 — Planning kinds stay in Planning

Given a person creates Epic, Refactor, and Plan rows, each appears in Planning with its Project icon
and is refused by both existing-Session and new-Session assignment routes. No execution phase or
assignment is made.

### WS2-C03 — Every card carries the Project presentation

Given two Projects with different icon grids and equal item titles, all-Projects and Project-scoped
reads return the right Project id/label/icon for each card. If one Project later becomes unreadable,
its item remains and says `project_unavailable`; it never borrows the other icon.

## 3. Human authority

### WS2-A01 — Assignment is person-only

For assign, reassign, unassign, cancel, reopen, Project change, kind change, and deployment-policy
change, a send-capable device succeeds where valid; orchestrator and task credentials receive a
typed refusal and write no item, assignment, event, receipt answer, or terminal effect.

Project/kind correction succeeds only before the first successful assignment. Once assignment
history exists, both are immutable even after unassignment or reopening; changing direction
requires a new person-created item. Deployment-policy changes remain versioned and nonterminal.

### WS2-A02 — Agent mutation is owner-scoped

Given Session A owns item X and Session B owns item Y, A may edit X's title/description,
documents, steps, condition, and lifecycle phase. A is refused on Y; B is refused on X; an
unassigned item refuses both. A stale live-session reading produces `owner_identity_unknown`, not
permission.

### WS2-A03 — Concurrent edits do not overwrite

Given a person and owner Agent both read version N, when one writes N+1, the other's N write is
refused with the current version. After reread it may deliberately apply its change. Title,
description, documents, steps, and reference images all have this control.

## 4. Assignment

### WS2-S01 — Assign an existing Session

Given an executable item and one live assistant Session resolved to the same Project, a person
assigns it. The item becomes `assigned`, exactly one active assignment is recorded, the Session's
assigned-item projection contains one row immediately, and the Agent to-do read returns that row.

Negative controls:

- target belongs to another Project;
- target identity is ambiguous or unreadable;
- target is not an assistant Session;
- item already has an active owner;
- target disappears between selection and the transaction.

### WS2-S02 — Existing-Session assignment does not interrupt work

Assign separately to Sessions observed `working`, `waiting`, and `unknown`. Each receives zero
terminal input while its assigned-item projection appears immediately. A Session positively
observed `idle` receives one courtesy brief; a failed courtesy send leaves
`assigned_unnotified` without rolling back ownership. A retry of the original assignment returns
its receipt and creates no second assignment.

### WS2-S03 — Assign a new Session

Given a valid assistant/model and Project, a person requests a new Session. The assignment intent
exists before terminal I/O; the item visibly enters `assigning`; successful composer detection and
one briefing resolve the conversation and move it to `assigned`.

Failure injections prove that terminal-open failure, composer timeout, and briefing failure leave
one visible failed assignment, the item back in `created` with `assignment_failed`, no active owner,
no invented replacement Session, and a safe retry path. No failure leaves the item silently
assigned.

### WS2-S04 — One owner per item, several items per Session

Concurrent attempts to assign one item result in one active owner and one conflict. Assigning three
different items to the same Session succeeds and its projection shows exactly three item rows.

### WS2-S05 — Offline is not automatic reassignment

When a complete terminal-source reading proves the owner is gone, the item keeps that owner and
phase, gains `owner_offline`, and appears on the Board. No rule reassigns, cancels, plans, or closes
it. An incomplete reading changes nothing.

### WS2-S06 — Reassignment and unassignment preserve unfinished work

Reassigning to an existing Session atomically removes the old projection, creates the new one, and
rejects a late old-owner write. While a replacement new Session is opening, the old owner and phase
remain active; an opening failure changes neither. Unassignment removes the projection and adds
`owner_required` while preserving phase, evidence, documents, and steps. Reassignment resumes from
that phase; it does not create a new item or erase the earlier owner history.

## 5. Lifecycle and evidence

### WS2-L01 — The main path is closed

For an assigned item, the owning Agent can traverse only:

`assigned → implementing → verifying → merging → deploying → done`.

Skipping a phase, using an unknown phase, or advancing a terminal item is refused. Every accepted
transition appends one event and increments one version. Person/device, other-Agent, and broker
attempts to advance or rewind the execution phase are refused without mutation. The `done`
transaction records the completing owner, releases its assignment, and removes exactly that
Session projection while retaining assignment/evidence history.

### WS2-L02 — Verification failure preserves evidence

From `verifying`, a failed verification record returns the item to `implementing`. The failed
command/check/reference remains in history. A second successful cycle can proceed without
rewriting the first.

### WS2-L03 — Merge requires valid landing evidence

An Agent cannot enter `deploying` merely by naming a commit. The broker accepts only evidence bound
to this Project/item and a permitted target. A pre-dispatch base commit, another item's commit, an
unverified branch, unreadable Git state, and a pending landing are each refused by name.

### WS2-L04 — Deployment is explicit

For `required`, done needs successful deployment and health evidence. For `not_required`, the item
still enters `deploying`, records that no deployment is required, and may then complete without
deploy evidence. For `agent_decides`, done needs either deploy evidence or an Agent-authored
`not_applicable` reason recorded in `deploying`. Empty reasons and unknown deployment readings are
refused.

### WS2-L05 — Conditions preserve the phase

At each nonterminal phase, setting `blocked` or `waiting_user` keeps the phase unchanged and makes
both fields visible. Clearing the condition restores the phase presentation. Broker-derived
`owner_offline`/`evidence_unknown` cannot be cleared by cosmetic Agent text.

### WS2-L06 — Reopen starts another cycle

A device may reopen done/cancelled work; an Agent may not. The new cycle retains all earlier
events/evidence, has a new cycle id, and is either unassigned or deliberately assigned according to
the person's request. No earlier task automatically binds to it.

## 6. Agent-owned content

### WS2-D01 — Title and description are editable, identity is not

The owning Agent and a person can edit title/description. Agent attempts to change Project, kind,
owner, creation actor, deployment policy, cancellation, reopening, or deletion are rejected even
if those fields are included beside a valid edit. The separate lifecycle command may still advance
a valid item through `done`.

### WS2-D02 — Documents are bounded references/content

The owner can add, update, order, and remove item documents inside registered count/size limits.
Repository-relative paths cannot escape the Project. A URL/path is never fetched as a side effect
of storing it. Oversize content or a full item is refused without eviction.

### WS2-D03 — Steps are item-local aids

The owner can add, reorder, complete, and reopen steps. Completing every step does not advance the
item; advancing the item does not silently complete steps. Steps never appear as Board cards or
Session assignments.

### WS2-D04 — Reference images are durable person-owned input

A send-capable person can add PNG, JPEG, GIF, or a platform-decodable raster source to a
nonterminal item. The stored bytes decode as normalized PNG; item/list responses contain only
metadata; the opaque image route returns those bytes for GET and none for HEAD. The same image is
readable from the hosted Console without borrowing a Session identity.

Adding and deleting each increment the item version and append `image.added` or `image.deleted`.
The same idempotency key replays one outcome, a stale version changes nothing, deleting an image
from another item is refused, and closing the item makes both mutations refuse until reopen.
Machine/orchestrator and Agent credentials cannot add or delete reference images even when they
own the item. They can read image metadata and bytes as item context.

The seventh image, a normalized image over 5 MiB, an item over 15 MiB, a store over 512 MiB, and a
request over 18 MiB are each refused without evicting or changing existing images. Corrupt,
non-raster, path, and remote-URL inputs are refused without fetching anything.

## 7. Broker boundary

### WS2-B01 — Dispatch never creates human work

Starting and completing tasks with no `work_id`, including successful landings and leftovers,
creates zero items, assignments, planning rows, proposals, and top-level assigned-item rows.

### WS2-B02 — A bound task is nested execution detail

A task carrying an existing item id and owned by that item's Session is shown under the item. Three
children create three detail rows and still one assigned-item row. Unknown, cross-Project, terminal,
or differently owned item ids are refused or recorded unbound according to the dispatch contract;
they never create a replacement item.

### WS2-B03 — Landing satisfies a guard but moves nothing alone

Recording valid landing evidence changes the evidence read model and enables the next lifecycle
request. Without an owning-Agent request, item phase and version do not move. Replaying the landing
does not append a duplicate item event.

### WS2-B04 — No clock mutates placement or closure

Advance the test clock past the former 24-hour, three-day, and seven-day boundaries. Items,
assignments, and proposals remain where they were. Diagnostics may report age; no Backlog move,
drop, unconfirmed closure, or rule proposal is written.

## 8. Proposals

### WS2-P01 — Agent proposal is a draft, not an item

An identified Agent may create one bounded proposal with Project, kind, title, description, reason,
and suggested acceptance. Counts show one pending proposal and zero new items. A broker rule,
landing, timeout, task result, or direct-to-do read cannot create a proposal.

### WS2-P02 — Person previews and edits before acceptance

The Console shows the proposed Project icon, kind, content, source Session, and references. A
person may edit the draft in the accept request. Acceptance creates exactly one `created` item with
the accepted bytes and links the proposal atomically. It does not assign the proposing Session.

### WS2-P03 — Reject and wait are safe

Reject creates no item and retains proposal provenance. Advancing the clock creates no item and
does not auto-accept or auto-reject. Capacity full refuses a new proposal and evicts none.

## 9. Session direct to-dos

### WS2-T01 — The plus control creates one unsent row

Given an open Session detail and a send-capable device, pressing `+`, entering text, and confirming
creates one direct to-do for that conversation. It shows no delivery mark, is ordered after older
open rows, and creates no work item/proposal/task.

Keyboard acceptance: focus enters the modal, Tab is trapped within it, Escape cancels without a
write, submit returns focus to the `+` control, and the narrow layout keeps all actions reachable.

### WS2-T02 — Send is person-only and exactly once

Pressing Send records durable intent and types the row once. Successful delivery sets `sent_at` and
shows `✓`. A retry replays the receipt. Machine/Agent calls to Send are refused. Busy, gone,
ambiguous, and unreadable targets each have typed outcomes and do not falsely set `sent_at`.

### WS2-T03 — Agent read returns all next work and produces the double mark

The owning Session's Agent read returns its assigned items and open direct to-dos, and atomically
sets each direct row's `read_at` once; the UI shows `✓✓`. Reading an unsent row moves directly from
no mark to `✓✓` and disables Send. A person viewing the panel does not mark it read; another
Session cannot read or mark it.

The two checks are visually overlapping and have localized accessible text for “read”. Unsent,
sent, and read remain distinguishable without color.

### WS2-T04 — Completion authority is narrow

The owning Agent and a person can check a row complete. The Agent cannot edit text, Send, Delete,
or reopen it. Completion is idempotent and retains sent/read timestamps.

### WS2-T05 — Delete is person-only and removes content

A person can Delete a direct to-do; it disappears from open/closed reads and the text is absent
from active storage. The audit records id/actor/time/outcome without copying the deleted text.
Agent, rule, and broker deletion attempts are refused.

### WS2-T06 — Pull never becomes an automatic wake

With a Session idle and an unsent to-do present, advance every broker/console tick. No terminal
effect is recorded or typed. A later Agent read sees it. Only a user Send produces immediate
delivery intent.

## 10. Session panel and Board UI

### WS2-U01 — The three groups cannot be confused

Given two assigned items, three direct to-dos, and four child tasks, the panel summary and markup
say “2 assigned items” and “3 direct to-dos”; tasks are nested/folded and never counted as five or
nine items. Each assigned row has Project icon, kind, title, phase, condition, and step count.

### WS2-U02 — Reassignment moves one projection

Reassign an item from Session A to B. After the write, A has no assigned-item row and B has exactly
one; the Board shows B. A late poll or stale event from A cannot bring the row back or advance it.

### WS2-U03 — Board areas match phases

At desktop and phone widths, Planning, Unassigned, Assigned, Implementing, Verifying, Merging,
Deploying, and Recently Done show only their matching records. Planning kind wins first, terminal
state wins second, and missing owner wins third; an unassigned in-progress item retains its last
phase badge. Every card shows the Project icon. Project filtering changes membership without
changing stored state.

### WS2-U04 — Edit and Delete are deliberate person actions

Every current card exposes Edit and Delete. Edit opens with the current title and description,
refuses blanks and stale versions, and keeps the modal open with its typed failure. Delete opens a
confirmation that names the item and explains that its execution audit remains. Confirming it
removes the card and its Session responsibility together by recording a person-owned cancellation;
an Agent or machine credential cannot invoke either person route. Escape, the close control, and a
backdrop press dismiss both dialogs without mutation, including at phone width.

### WS2-U05 — One authoritative Board

Navigation exposes one Board. Project links open it with a Project filter. The retired Project
Board is absent from navigation and no current card/count is read from its source. The Now page may
summarize the same v2 ids/versions but cannot write a duplicate state.

## 11. Capacity and read failures

### WS2-K01 — Every new bound is registered

The capacity guard fails before registration for open items, planning items, proposals,
assignments, documents, reference-image count/bytes/request size, steps, direct to-dos, and page sizes; it passes after all rows name their
limit, behavior at full, diagnostics reading, and notification class.

### WS2-K02 — Full means refuse, not evict

For each evidence/person-work table, inject a small limit, fill it, and attempt one more write. The
new write is refused with capacity detail; existing rows and ordering are byte-for-byte unchanged.

### WS2-K03 — Partial reads name the gap

Inject unreadable Project, Session identity, Git evidence, and one malformed item-related row. The
response names each gap and does not answer empty, absent, unassigned, merged, deployed, or done.

## 12. Destructive cutover

### WS2-X01 — Reset cannot run accidentally

Ordinary startup, schema migration, Agent credentials, remote devices, and an incorrect
confirmation cannot delete v1 content. The administrative operation is local-only, first answers a
dry-run with exact table counts/file targets, and requires confirmation bound to that exact set.

### WS2-X02 — Reset removes all and only old Board content

Seed v1 work, board, backlog, moves, proposals, Board-related decisions/digests,
dispatch-derived to-dos, and positively identified legacy Board files. Also seed transcripts,
broker tasks, landings, Git/worktree facts, settings, credentials, Project catalog/icon data, and a
foreign file beside legacy Board files.

After reset, every v1 Board subject is absent; every retained subject and the foreign file is
byte-for-byte intact. No content backup is produced. The receipt contains only target names,
counts, time, and outcome.

### WS2-X03 — Old facts cannot regrow the old Board

Restart and run every beat/reconcile/sweep against retained old tasks and landings. All v2 lists
remain zero, no dispatch-derived to-do appears, and no legacy Board source is reopened. A new
person-created v2 item then works normally.

### WS2-X04 — Failed reset is legible and recoverable

Inject failure before the transaction, during database deletion, before legacy-file deletion, and
after database commit. The operation reports which boundary committed, never claims full success
for a partial result, and a same-confirmation retry converges without touching retained data.

## 13. Release gate

The feature may be called implemented only when:

- every scenario above has an automated test with its negative controls;
- all repository-required Go, cross-platform, contract, privacy, legacy-copy, and web checks pass;
- a clean throwaway daemon passes the destructive-reset drill;
- desktop and phone e2e passes include keyboard/focus assertions;
- the live cutover dry-run is reviewed before the one authorized destructive execution;
- post-cutover reads prove zero old content and no regrowth;
- documentation changes status from “approved target” to “implemented” only after those facts.
