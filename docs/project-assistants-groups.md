# Bot groups and team management

Status: **runtime and store wired; no route, tool, console screen or host
composition yet.** Groups install, export, run their room turns through the
Bot runtime and answer the person's Service operations. Nothing reaches a
person until the host composes them (`projectassistanthost`), HTTP and MCP
expose them and the console shows them; the exact remaining hooks are in the
task's integration notes.

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

## Limits

Registered in `internal/domain/capacity` with Codync's values: rounds 3,
replies 10, late turns 3, member wait 600 s, history lines 24, characters per
shown line 2000, Bots a manager created 20. Codync has **no member maximum**;
`DefaultGroupLimits` passes the runtime's actor bound, since a room asks
members one at a time through runtime actors.

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

## What this does not prove

The tests use a scripted provider and a real SQLite database. They do not
show a native Claude Code or Codex member answering in a room, a person using
a route or screen, Cloud delivery, or manager cards waiting and expiring.
Those need the host composition, routes and their own acceptance.
