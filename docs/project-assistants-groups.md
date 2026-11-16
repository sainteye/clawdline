# Bot groups and team management

Status: **API 76 transport, manager coordination and console implementation
prepared; host integration and interactive UI acceptance remain pending.**
The integration handoff includes the Host/SQLite wiring and its tests as a
separate patch, because those paths belong to the integrating root. Scoped
HTTP tests use real TCP, SQLite and a scripted provider. A task-private copy
with the wiring patch also exercises the actual Host. Neither result proves
that the running daemon or hosted console has been updated.

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
- a duplicate or unknown member is refused. Codync drops them silently; here
  the stored list is exactly what was asked for, so a replayed command cannot
  mean something else.

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
`pa.SchemaVersion`: folding them in would need a 3→4 feature migration and
nothing in export or import needs a shared number. `app.InstallSchemas`
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

## Limits

Registered in `internal/domain/capacity` with Codync's values: rounds 3,
replies 10, late turns 3, member wait 600 s, history lines 24, characters per
shown line 2000, Bots a manager created 20. Codync has **no member maximum**;
`DefaultGroupLimits` passes the runtime's actor bound, since a room asks
members one at a time through runtime actors.

Manager approval detail is at most 2000 UTF-16 units, matching JavaScript and
Codync's `encode_utf16` count. A plan waits at most 270 seconds. Up to the
existing owned RPC concurrency ceiling (32) may wait on one Host, at most one
per manager. These three bounds are registered and measured separately; a
Host without a live occupancy measurement reports that gap rather than zero.

## Team management policy

`internal/app/projectassistant/team_management_policy.go` is a pure guard
mirroring Codync's `manage_team`:

- the person's switch (`Bot.ManageTeam`, read as a grant by `GrantFor`
  under the Bot's version) is off by default and only the person's settings
  change it; `Bot.CreatedBy` is set only by the host; every call checks that the caller's own grant
  is enabled and that the caller is in the current full Project;
- argument names are allowlisted (name, instructions, color, shape, hidden);
  authority fields — manage switch, creator, approval policy, mode, agent,
  folder, model, fallbacks, routines, deletion — are refused before a card;
- a created Bot inherits the manager's Project, approval policy and mode,
  manages the team too and records the manager as its creator. A different
  policy or mode is refused, not ordered: no ordering of policies across
  agents is proved. Another agent is allowed only when the manager works in
  Auto mode under Ask; `auto_read` is not Codync's Auto;
- after approval and before the mutation, `ConfirmManagerPlan` refuses a
  plan whose grant changed while the card waited, including off and on again.

The plan is data. The host creates the Bot through a separate manager
operation, never through the person's `Service.Create` or `Service.Edit`.

`TeamCoordinator` owns the bounded pending cards. Its identity comes from the
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
present, full Project, inherited authority and machine-wide creation count
in the **same transaction** as the Bot mutation. A separate check followed by
ordinary `Repository.Commit` leaves a revocation race and does not implement
this port. Host-side `CreatedBy` survives edits. An invalid plan rolls back
its Bot, receipt, events and roster projection together.

The card shows the whole proposed change and inherited authority. Oversized
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
  Routines, agent/model selection, fallbacks, Project changes and authority
  settings remain outside these tools. `list_team` returns same-Project setup,
  including hidden Bots, without messages, memory, paths or native identities.
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
