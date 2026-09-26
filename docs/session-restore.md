# Restoring the sessions a reboot took away

## The problem

When the machine reboots or crashes, every open Claude Code and Codex session goes with it: the
tmux server is gone, and so are the iTerm2 tabs. Each conversation is still on disk and can be
resumed one at a time (`POST /v1/places/{place}/resume/…`), but nothing recorded *which*
conversations were open, so the person had to remember them and find each one again.

The daemon now keeps that record and offers the conversations back after a restart.

## The rule: a reboot is a new boot id

A daemon restart and a machine restart look the same from inside the daemon: it starts, and it
reads the machine. They mean opposite things. After a daemon restart the tmux panes and iTerm2
tabs are still where they were, and offering them back would open every session twice. After a
machine restart they are all gone.

The operating system names each boot, and that name is what tells them apart:

| Platform | Source |
| --- | --- |
| macOS | `sysctl -n kern.bootsessionuuid` |
| Linux | `/proc/sys/kernel/random/boot_id` |
| Windows and others | none — the feature is unavailable |

`internal/adapters/bootid` reads it. **Unknown is not a value**: when the id cannot be read, the
daemon records nothing and `GET /v1/sessions/restorable` answers `available: false` with
`reason: "boot_unknown"`. A guessed id would either merge two boots (nothing offered after a real
reboot) or split one (sessions that are still open offered again).

## The record

Two tables in the daemon's store:

- `restore_boots` — one row per boot id, with when it was first and last seen.
- `restore_sessions` — one row per boot and conversation: assistant (`claude` or `codex`),
  working directory, place id, title, terminal backend, first and last seen, when it went
  (`gone_at`), when the person closed it through Clawdline (`closed_at`), and the person's answer
  (`restored`, `dismissed`, or none yet).

Every scan of the machine the daemon takes is shown to the recorder
(`app.InventoryReading.Observe`). The broker's beat takes one every five seconds whether or not a
console is open, so the record follows the machine with nobody looking and costs no scan of its
own. For each reading:

- An **incomplete** reading changes nothing. A source that did not answer is not evidence that
  its sessions closed.
- A **complete** reading writes every session it shows that has a conversation id, a working
  directory and an assistant of `claude` or `codex`, and marks each of the boot's rows it does
  **not** show as gone at that reading's time. The row itself stays, with everything it held. A
  gone row seen again in the same boot is open again (`gone_at` cleared).
- The broker's own children are left out (a task names the terminal as its child's and the
  process in it is that task's, `ownChild`). A child belongs to its task; reopening it outside
  that task is wrong.
- The store is written only when the set changes, or when the recorded `last_seen` is five
  minutes old, so a row's `last_seen` says roughly when it was last seen open. Between writes, a
  complete reading moves the **boot's** `last_seen` alone at most once a minute — one row update,
  the heartbeat the grace line below is drawn from.
- A close through Clawdline (`Actions.Close`, the one that records `session.closed`) marks the
  row `closed_at` and gone. A scan that began before the close and still shows the terminal does
  not undo it; one that began after it (the person resumed that conversation) does.
- Past 200 rows in a boot, the rows that went longest ago are dropped first.
- Only the current boot and the most recent previous boot are kept.

The title is the manual title the session list would show when there is one, else the session's
own name.

### Why a session that goes is not deleted

The first version replaced the boot's rows with each complete reading, so a complete empty reading
emptied them. That is exactly what a normal restart produces. macOS quits applications and
terminates processes while the daemon can still be scanning: once iTerm2 has quit, its source
answers complete and empty (`iterm_darwin.go` treats "not running" as an observed absence, not a
failure), and once the tmux server is killed, tmux answers `NoServer`, which is also complete and
empty. One reading in those last seconds erased the record the restart needed, so nothing was
offered after the case the feature exists for. A crash or a power loss never hit this; a shutdown
very likely did.

Keeping the row and its `gone_at` lets the reader tell the two apart after the reboot.

## What is offered

The **restorable set** is the most recent boot that is not the current one, less the rows
already restored or dismissed, less the rows closed through Clawdline, less the conversations open
right now, and of what remains only the rows that were open at that boot's end:

- rows never seen to go — a crash or a power loss took them with no reading in between; and
- rows that went no more than **3 minutes** before the boot was last seen (`gone_at >=
  last_seen − grace`) — the shutdown's final wave, in which apps quit and the tmux server dies
  over a few seconds.

A row that went earlier than that was closed by the person well before the reboot and is not
offered. A daemon restart inside the same boot offers nothing.

## The routes

Contract: `api/v1/restore.schema.json`.

| Route | Who | What |
| --- | --- | --- |
| `GET /v1/sessions/restorable` | any paired device | `{available, reason?, previous_boot_last_seen?, sessions: [{conversation_id, assistant, place, place_label, cwd, title, last_seen}], at}`, most recently seen first |
| `POST /v1/sessions/restorable/restore` | a device that may send, with an `Idempotency-Key` | body `{conversations: [id…]}`; one result per distinct id: `{conversation_id, ok, code?, message?, id?, backend?, attach?}` |
| `POST /v1/sessions/restorable/dismiss` | the same | body `{conversations?: [id…]}` (absent means every row on offer); answers `{ok, dismissed, at}` |

A restore opens each conversation through the same opening queue and the same `Starter.Resume` a
person's resume uses; there is no second launcher. A row that opened is marked `restored`. The
per-row codes are `not_restorable`, `place_unavailable`, `conversation_not_found`, `open_failed`
and `over_capacity`. A retry with the same key is answered with the first answer; the same key
with a different body is refused `idempotency_key_reused`.

Clawdline Cloud carries the same three as the relay words `restorable-sessions`,
`restore-sessions` and `dismiss-restorable`.

The bounds — rows per boot, boots kept, conversations per restore, how stale `last_seen` may grow,
the boot's heartbeat and the grace window — are registered and listed in [limits.md](limits.md)
(N49).

## What is not covered

- **A tmux `kill-server` or quitting iTerm2 without a reboot offers nothing.** The boot id is the
  same, so the rows are only marked gone; nothing is offered until a reboot, and after one they
  are offered only if the kill was within the grace window of the boot's end.
- **Closing a session outside Clawdline in the last three minutes before a restart looks like the
  shutdown.** A tab closed by hand, or an assistant exited with `/exit`, just before rebooting is
  inside the grace window and is offered; the person can dismiss it. A close through Clawdline is
  recorded and is not offered.
- **A daemon stopped long before the machine went down draws the line from when it stopped.** The
  boot's `last_seen` is the daemon's last complete reading, so sessions that were open then are
  offered even if they were closed while the daemon was not running.
- **The model is not recorded.** A restored session starts with the assistant's default model.
- **A reading that is never complete records nothing.** Completeness is the AND of every source;
  a machine whose iTerm2 cannot be asked never produces a complete reading, and nothing is
  recorded there until it can be.
- **A conversation the assistant no longer lists for its directory cannot be restored**
  (`conversation_not_found`); the resume path only opens conversations it can list.
- **A manual title is not copied onto the new terminal.** The title store matches a manual title
  by conversation id before terminal id, so it follows the restored session only if the assistant
  keeps the conversation id when it resumes, and only while the title row is inside its retention
  window. Whether each assistant keeps the id on resume was not measured for this change.
