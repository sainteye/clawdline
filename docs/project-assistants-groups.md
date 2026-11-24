# Bot groups and team management

Status: **The existing API 76 routes are wired through the Host. API 88 adds
source-owned exact quotes, batch previews and consistent room activity;
deployment and interactive acceptance of that extension remain pending.**
Scoped tests cross the closed Cloud decoder, real TCP HTTP and SQLite with a
scripted provider. These source tests do not prove that the running daemon
or hosted console has been updated. GroupRoom layout, mention selection,
member navigation and desktop/phone acceptance are separate work.

The design follows Codync's group chat (`host/src/chat/group.rs`,
`chat/group/rooms.rs`) and team management (`chat/manage.rs`), pinned at the
revision recorded in the integration notes.

## A group is a room, not a Bot

`pa.Group` (`internal/domain/projectassistant/groups.go`) has a
source-qualified key, the full `ProjectScope`, its own conversation, an
ordered member list, a version and a tombstone. It has no Agent, backend,
folder or native session. A member answers in its own conversation under its
own execution fence; the room only decides who is asked and whether an answer
is still wanted. A group's key never equals a Bot's.

Membership is validated against the Bots actually stored on the source
machine, in the same transaction as the write:

- every member is a live Bot of the **same full Project** — the same
  canonical repository registered as another Project is refused;
- no member from another machine, no group as a member (no nesting), the
  group is not its own member, the list is not empty;
- a hidden Bot is a valid member; a deleted Bot is not;
- duplicates are deduplicated in input order, as in Codync; an unknown member
  is refused rather than silently dropped.

Each member records the membership revision it joined at. Removing a member
and adding it back gives it a new join revision, so anything asked of it
before it left cannot publish. Creating a group whose members are exactly
those of a live group of the same Project opens that group instead (Codync's
rule); editing is not deduplicated, as upstream.

Deleting a group tombstones it and moves its room epoch. Member Bots, their
conversations and their own chats are untouched. Deleting a member Bot takes
it out of every group in the same transaction that deletes its row
(`Store.DeleteBot`); as upstream, a group may be left with no members, and
then refuses to start a room turn.

## Storage, install and export

The group tables (`project_assistant_group_*`, including the room's lines)
are versioned on their own (`GroupSchemaVersion`, now 1), not by
`pa.SchemaVersion` (currently 11): the optional API 88 JSON fields need no
physical database migration. `app.InstallSchemas`
installs the feature schema and then the group tables; the group installer
refuses to run before the feature schema exists.

A Bot's export carries the groups it belongs to, or belonged to when they
were deleted (`Group.Deleted` marks the tombstone), with members and room
state. Restore refuses a live group membership (`group_transfer`): a
single-Bot archive cannot authorize a room naming other Bots, as with
exchanges. A tombstone does not block restore and is not restored.

## Room turns

`pa.RoomState` is persisted per group and written under a revision CAS:

- the person's message starts a room turn, replacing the one running and
  restoring the late-turn allowance;
- a member the room did not wait for (busy elsewhere, or overdue) answers
  late; who it @-mentions gets a late room turn, at once or when the running
  one ends, at most `MaxLateTurns` per message of the person's;
- the group's Stop moves the epoch, drops pending late answers and allows no
  late turn until the person writes again.

A member's queued room turn carries a `pa.RoomTicket`: group, member, epoch,
join revision and whether it is late. It is checked before the turn runs and
again before the answer is published. A non-late ticket is refused once a
newer room turn started; a late ticket survives newer turns but not the
group's Stop; any ticket is refused once its member left, rejoined, or its
Bot was deleted, or the group was deleted. The member's own-chat Stop keeps
late room turns and drops the rest (`AfterOwnChatStop`); the group's Stop
drops every one for that group (`AfterGroupStop`); asking a member again
replaces its turn in that room (`AskAgain`).

Within a room turn, members answer one at a time. Round 0 asks who the
person's message @-mentions or replies to, or everyone (also on `@all` or
`@everyone`). Later rounds, and late turns, ask only members another member
named since and who have not spoken or been shown that line since. Each round
starts one member later. A member that answers exactly `(pass)` publishes
nothing; a round where nobody spoke ends the turn, as do `MaxRounds` and
`MaxReplies`. An answer's author is the ticket's member, never a name in the
text.

## How a room turn runs

A room turn is an ordinary host-notice turn of the member's own conversation,
under the member's own fence and actor, with the ticket as `Turn.Room`. The
runtime (`runtime_groups.go`) admits the ticket on submission and again when
the turn is dequeued, before any native effect, so a refusal ends it failed,
never unknown. A permission card raised in that turn is the member's own,
filed under its fence, and is answerable only while the ticket still holds.
The turn finishes through `Store.FinishRoomTurn`: the member's terminal facts,
the last ticket check and the room line commit in one SQLite transaction. A
ticket that no longer holds withholds the line and still finishes the member's
own turn; a failed line write rolls back both.

`RoomDriver` asks speakers one at a time and waits for each at most the
registered member wait. A member busy with a turn outside this room answers
late, on its own. An overdue member is recorded in `RoomState.Overdue`, so its
answer still lands, as a late one. A refused, failed or unknown member turn
counts as a pass and is never resent, re-asked under another agent, or sent
again under a new key. The group's Stop drops members' queued turns in that
room and stops an active one there; their turns elsewhere are untouched.

The member's prompt follows Codync's `member_prompt`: who is in the room, how
to answer and pass, and the room's newest lines since the member last spoke,
each clipped. Deliberate differences: it shows the newest 24 lines within that
window (Codync shows the oldest 24 after the member's last line), and it is
built when the member is asked, not refreshed when its turn starts.

## The person's operations

`Service.CreateGroup`, `EditGroup`, `DeleteGroup`, `StopGroup` and
`SendGroup` (`service_groups.go`) authorize with the existing human authority
for the group's key and re-check it immediately before the store write;
`SendGroup` re-checks it after the room read, as `Service.Send` does. The
person's room message carries a command and receipt: a retry replays without a
second line or room turn. `GroupHistory` and `Groups` read with the person's
read authority, separately from any member's execution; a read-only device
reads the room and is refused writes.

API 76 adds the following subroutes under the existing Bot prefix. Every
write has the existing `command` envelope and matching `Idempotency-Key`;
every read names the captured source machine.

The human create boundary assigns a source-owned group key from the durable
principal, source and idempotency key. A client-supplied candidate ID cannot
choose the stored identity; retries retain the committed group and its
conversation. The trusted host `CreateGroup` port remains unchanged.

| Method | Path below `/v1/project-assistants` | Cloud word suffix |
| --- | --- | --- |
| GET / POST | `/rooms/groups` | `groups` / `group-create` |
| GET / PATCH / DELETE | `/rooms/groups/{group}` | `group-history` / `group-edit` / `group-delete` |
| POST | `/rooms/groups/{group}/messages` | `group-send` |
| POST | `/rooms/groups/{group}/stop` | `group-stop` |
| GET | `/{bot}/team-plans` | `team-plans` |
| POST | `/{bot}/team-answer` | `team-answer` |
| POST | `/{bot}/team-settings` | `team-settings` |

The nested `rooms/groups` namespace preserves existing Bot IDs, including a Bot named `groups` or `rooms`.
The full words start with `project-assistants.`. Group history uses
`before_seq` and `page_size`; lines are ordered oldest to newest inside the
page. `author` is present only on a member line, and `reply_to_author` retains
the member a person replied to. A group list includes tombstones so clients
can converge; the console hides them. A complete reply must fit the existing
Cloud plaintext envelope. History keeps the newest whole lines that fit and
continues before the oldest retained sequence; it never truncates a line. A
single oversized line, or another oversized complete reply, is refused.

### Exact replies and consistent read context (API 88)

`group-send` optionally accepts `quote: {seq}`. The source resolves that
positive sequence **inside the same room and write transaction** as the new
message. Only a person line or member answer can be quoted. The client cannot
supply the original author, text or another room. The resulting entry retains
`quote: {seq, kind, author?, text, truncated?}` separately from the person's
new text; a member quote also derives `reply_to_author`. A conflicting legacy
author is refused. Mentions and the quoted author both count when selecting
first-round speakers. A repeated command returns the original stored entry
without another turn; changing its quote conflicts with the original command.
Omitting `quote` preserves the previous canonical command digest.

Quote excerpts reuse `MaxMessageBytes`, preserve valid UTF-8 and mark any
truncation. The complete original line stays at the quoted sequence. The
member prompt identifies this source-resolved quote independently and uses
the existing `PeerLineRunes` budget. No new quote capacity or model call is
introduced. Sources below API 88 retain ordinary API 76 group sends; the
captured source client refuses exact quotes before transport on those sources.

Group history obtains metadata, room epoch, lines and `activity` in one SQLite
read transaction. Activity names actual accepted, queued, running or
permission-waiting member turns whose current membership, room ticket and
execution fence still hold. A group marked running alone does not identify
which member works. Removed/rejoined members and stopped tickets cannot claim
activity. The SQL projection reads compact turn and conversation metadata,
not every member's full export or message history.

Each list group may include `room` and `last_entry`, from one batch SQL query.
The newest stored line supplies its author and optional `at`; newly written
person and member lines record source time. Historical undated lines stay
undated. Group `updated_at` remains metadata time and is not last-message
time. SQL lengths limit optional data loaded to the caller's existing Cloud
read budget. HTTP then fits optional encoded fields into the actual existing
plaintext reply envelope. An oversized preview is omitted while base metadata
and other fitting previews remain readable; no per-room history fanout or
invented continuation is added.

`GroupPublication` captures and rechecks the exact human grant after JSON
encoding. A revoked or replaced grant cannot publish an earlier read. The
console's source client checks API 76, source identity and access before
transport and rechecks after it. A write that crossed transport and then
lost its grant becomes `pa_outcome_unknown`; it is not retried automatically.

The roster opens groups as rooms, with a member list, per-line authors,
earlier-message paging, member replies, Stop and explicit deletion
confirmation. Creation and editing offer Bots from the same full Project.
Read-only devices can see rooms and pending team cards; write controls are
disabled and HTTP independently refuses their actions. Being paired does not
itself make a device read-only.

The room mounts a portal directly inside the surrounding `.pa-shell`, so a
roster scroll container cannot make it a narrow row below the list. The
optional `active` prop defaults to true; a parent opening a member's chat
keeps the room mounted with `active=false`. Hidden rooms stop reads while
retaining their draft, exact quote, history and reading position. On return
the room restores its own focus and position. The parent must clear any
previous Bot selection when opening a room, and must not also restore roster
focus when returning from a member chat. `onOpenMember` accepts the actual
source-qualified Bot key; navigation is read-authorized independently from
content-write authority. Parent roster wiring and that complete navigation
journey require their own integrated browser check.

The composer filters current members after a trailing `@`. Up/down changes
the highlighted member; Tab or Enter inserts its full name; Escape dismisses
the list until the draft changes; touch selects the same option. A completed
name followed by a space, and `@all`/`@everyone`, allow the next Enter to send.
IME composition never sends or picks. The client edits only the draft; the
source selects speakers. Replies on person and member lines keep their exact
quote visible with author and excerpt. Working indicators name only the
source's valid activity tickets. Opening the member panel, reading earlier
pages and receiving newer lines retain the current reading anchor.

## Limits

The first round of a person's message can ask every addressed member. Later
rounds and turns triggered by late Bot replies count up to 10 automated replies
per run. A run ends after three rounds or a round without a reply. Membership
is independent of active runtime workers, since the room asks members in
order. The existing limits for late turns (3), member wait (600 s), prompt
history (24 lines), characters per shown line (2000), and Bots created by one
manager (20) are registered in `internal/domain/capacity`.

Manager approval detail is at most 2000 UTF-16 units, matching JavaScript and
Codync's `encode_utf16` count. A plan waits at most 270 seconds. Up to the
existing owned RPC concurrency ceiling (32) may wait on one Host, at most one
per manager. These three bounds are registered and measured separately; a
Host without a live occupancy measurement reports that gap rather than zero.

## Team management policy

`internal/app/projectassistant/team_management_policy.go` is a pure guard
mirroring Codync's `manage_team`:

- the designated lead can manage its team from API 103. Other Bots use
  the person's explicit switch (`Bot.ManageTeam`), which defaults to off.
  `GrantFor` checks both under the Bot's settings version, independently of
  trace, chat and permission changes. The console hides the
  switch for a lead on a source that grants access by role and shows pending
  approval cards using the effective grant in the Bot response. Every call
  checks the grant and the current full Project. Only the host sets
  `Bot.CreatedBy`;
- `create_bot` defaults to the manager's Project. An optional `folder` names
  another registered Project on the same machine by its absolute path or
  unique folder name. The host resolves the destination through the Project
  registry and rechecks it when the person approves. The designated lead can
  list and propose edits to existing Bots in any Project on that machine,
  including personal workspaces. Other managers can list and edit Bots in
  their current Project. `update_bot` uses the Bot ID returned by `list_team`;
- a created Bot inherits the manager's agent, approval policy, mode and
  explicit team grant. The lead's role grant applies only to the lead. The
  new Bot records the manager as its creator. A different
  policy or mode is refused, not ordered: no ordering of policies across
  agents is proved. Another agent is allowed only when the manager works in
  Auto mode under Ask; `auto_read` is not Codync's Auto;
- after approval and before the mutation, `ConfirmManagerPlan` refuses a
  plan whose grant changed while the card waited, including off and on again.

The plan is data. The host creates the Bot through a separate manager
operation, never through the person's `Service.Create` or `Service.Edit`.

From API 110, the active turn records a team approval on the same permission
channel as other Bot actions. It is anchored after the message sequence observed
when offered. A requesting console includes it in chat history and updates the
same card from permission events. The roster uses the existing needs-input
state. The host still requires an explicit human choice under every native
permission policy. The live call owns the decision and cannot be restored from
a historical card. Managers using a client that waits for the first HTTP byte
receive an MCP event stream immediately, with the JSON-RPC result after the
decision. The `team-plans` and `team-answer` compatibility routes read and
answer the same live review.

`TeamCoordinator` owns the bounded pending decisions. Its identity comes from the
actual running turn's Host-bound native tools, never from arguments. The
owned MCP server advertises `list_team`, `create_bot` and `update_bot` only
while `TeamAvailable` validates the current grant, and checks again on every
call. There is no manager delete tool. A peer Bot review request cannot use
the management tools. A room-origin manager call also needs its room ticket
to remain valid.

The managing call waits without retaining an actor mutex or SQLite
transaction. The person's `AnswerTeamPlan` checks the human write authority;
acceptance repeats the execution, grant and deadline checks before calling
the dedicated `ManagedRepository.CommitManaged`. The SQLite implementation
must check the manager grant, active turn and binding, room ticket when
present, the approved destination for creation or the current role and target
Project for edits, inherited authority and machine-wide creation count
in the **same transaction** as the Bot mutation. A separate check followed by
ordinary `Repository.Commit` leaves a revocation race and does not implement
this port. Host-side `CreatedBy` survives edits. An invalid plan rolls back
its Bot, receipt, events and roster projection together.

The card puts the target Bot, destination and decision controls in the compact
view. Opening its details shows the full proposed change and inherited
authority, including the registered folder for an explicit creation
destination or a cross-Project edit when the folder can still be resolved. Oversized
detail is refused before a card appears. Declining, expiration, a cancelled
owned execution, a revoked grant or changing the grant off and on again
changes nothing. Pending cards are memory-only: a daemon restart cannot
recreate an executable approval. A stable execution-bound idempotency key
returns an already committed Bot instead of proposing another creation.
Cancellation or timeout before mutation admission withdraws the plan. Once
an approved mutation is admitted, a lost native waiter reports an unknown
outcome and retains its operation key; cancellation is not proof of rollback.
Each new approval attempt has a separate host-generated capability ID. An
old expired or revoked card cannot approve a replacement attempt, even when
the managing call reuses its durable operation key.

Deliberate differences from CodyNC 9f67eb09:

- Every managed mutation requires the person's explicit approval, including
  managers configured to approve ordinary agent tools automatically.
- Manager fields are limited to name, instructions, appearance and visibility.
  Routines, agent/model selection, fallbacks, moving existing Bots and authority
  settings remain outside these tools. `list_team` returns the authorized
  setup, including hidden Bots, with an optional name query. For the lead,
  entries include the registered Project folder when it can still be resolved.
  Chat, memory, private background workers and native identities are excluded.
- The person's Manage the team switch uses its own complete request shape,
  so `false` survives JSON normalization and older edits preserve the grant.
- The grant uses the Bot aggregate version. Any intervening aggregate change
  conservatively invalidates the card, even when it did not widen authority.
  A new turn re-advertises the current MCP capabilities; revocation refuses
  calls immediately, including calls from an older tool list.

## What this does not prove

The HTTP/SQLite suite verifies group authors, replay, editing, stopping,
deletion, other-Project refusal, read-only refusal and publication after a
grant change. Manager tests exercise approval, decline, revocation, changed
grants, deadlines and the UTF-16 boundary. The root wiring patch's Host and
store tests cover the actual composition and transactional refusals.

The Cloud tests cross the generated decoder and real HTTP boundary; the
golden update adds only the ten new words and preserves every older case.
This is the decrypted boundary, not production pairing, relay encryption or
a deployed Cloud acceptance.

`TestGroupRoomsHeadlessUI`, enabled with `CLAWDLINE_GROUP_UI_ARTIFACTS`, runs
the actual roster and room components in English/Traditional Chinese,
desktop/mobile, local/decrypted-Cloud, and read-only/old-source modes. It
uses an already installed Playwright headless shell and installs nothing.
The child sandbox refused Chromium's macOS Mach-port registration before
navigation; interactive UI acceptance therefore remains pending a root run
in the normal environment. No real Claude Code or Codex model was called.

## Observing an accepted room turn

An accepted send keeps the active room's serialized read follower alive while
the document is hidden. Opening a room whose consistent snapshot is still
running does the same. The existing refresh interval and source authority
remain in force; reads never replay the send or change its idempotency key.
A terminal room snapshot ends hidden observation. Idle hidden rooms do not
poll, and pagehide, leaving the room or revocation stops the follower. A typed
read failure remains visible, ends hidden continuation and preserves the
accepted person line unless authority requires clearing it. Foreground return
can reconcile the same stored room without sending another question.

A controlled HTTP/SQLite fixture reproduces the old boundary with a hidden
document: the source has both member answers and is terminal while the UI
keeps only the person line and a queued member. This does not prove the
visibility state of a production browser whose read surface does not expose
that property. Native completion, source publication and UI observation must
be recorded independently.

When members take turns after a person speaks, the first eligible member is asked to acknowledge or answer the message, including a greeting. If that member passes, the next eligible member receives the same responsibility until someone answers. This uses the existing turn selection and retry limits. A quiet room with an unanswered person message shows a status outside the transcript.
