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
  working directory, place id, title, terminal backend, first and last seen, and the person's
  answer (`restored`, `dismissed`, or none yet).

Every scan of the machine the daemon takes is shown to the recorder
(`app.InventoryReading.Observe`). The broker's beat takes one every five seconds whether or not a
console is open, so the record follows the machine with nobody looking and costs no scan of its
own. For each reading:

- An **incomplete** reading changes nothing. A source that did not answer is not evidence that
  its sessions closed.
- A **complete** reading replaces the current boot's rows with the sessions it shows that have a
  conversation id, a working directory and an assistant of `claude` or `codex`. A complete empty
  reading empties them: the person closed everything and there is nothing to offer.
- The broker's own children are left out (a task names the terminal as its child's and the
  process in it is that task's, `ownChild`). A child belongs to its task; reopening it outside
  that task is wrong.
- The store is written only when the set changes, or when the recorded `last_seen` is five
  minutes old, so `last_seen` says roughly when the machine was last seen with those sessions.
- Only the current boot and the most recent previous boot are kept.

The title is the manual title the session list would show when there is one, else the session's
own name.

## What is offered

The **restorable set** is the most recent boot that is not the current one, less the rows
already restored or dismissed, less the conversations open right now. A daemon restart inside
the same boot therefore offers nothing.

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

The bounds — rows per boot, boots kept, conversations per restore, and how stale `last_seen` may
grow — are registered and listed in [limits.md](limits.md) (N49).

## What is not covered

- **A tmux `kill-server` without a reboot is not detected.** The boot id is the same, so the
  daemon reads it as "the person closed everything" and the next complete reading empties the
  record. The same is true of quitting iTerm2.
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
