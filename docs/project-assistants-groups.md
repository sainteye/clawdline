# Bot groups and team management: the foundation

Status: **not wired.** This page describes modules that exist and are tested,
but no route, tool, schema registration or console screen uses them yet. A
person cannot create a group, and no Bot can manage the team, until the host
integration listed at the end lands.

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
it out of every group (`GroupStore.LeaveGroups`); as upstream, a group may be
left with no members, and then refuses to start a room turn.

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

## Limits are supplied

`pa.GroupLimits` and the manager's `ManagerLimits` are constructor or
function inputs; every field must be positive and nothing has a default.
Codync's values are rounds 3, replies 10, late turns 3 and created-by-Bot 20.
Codync has **no member maximum**; `MaxMembers` must come from a bound the host
registers in `internal/domain/capacity`, derived from its own Bot and process
budgets.

## Team management policy

`internal/app/projectassistant/team_management_policy.go` is a pure guard
mirroring Codync's `manage_team`:

- the person's switch (`TeamManagerGrant`) is off by default and only the
  person's settings change it; every call checks that the caller's own grant
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

Pure transitions and a SQLite repository cannot show that a member's turn
actually runs under its own fence in a room, that permission cards route to
the member that asked, that a room line is written atomically with its final
ticket check, or that manager cards wait and expire. Those need the host
integration and its own acceptance.
