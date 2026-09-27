# Archiving a Session and bringing it back

## The problem

A Session that has been quiet for days still holds its assistant's memory for as long as its
process runs. Closing it gives the memory back, but nothing then remembers that the conversation
was worth coming back to: the person has to find it again in the place's past list, and in a busy
directory that list stops long before a conversation from last month.

Archiving is a close that remembers. The Session's process is closed through the ordinary close
path, and the daemon keeps a row naming the conversation, so the person can bring it back at any
time with the same resume a person's resume uses.

## The record

`internal/adapters/store/session_archive.go`, one SQLite table `session_archive`, one row per
conversation id: `conversation_id`, `assistant`, `cwd`, `place`, `title` (the label the Session row
showed), `persona`, `backend`, `archived_at`. Archiving the same conversation again replaces its
row. Rows go when a restore opened the conversation, or past the bound, the one archived longest
ago first.

`internal/app/session_archive.go` is the rule:

- **Archive is `Actions.Close`, then the row.** The same closeability check, `close_blocked` unless
  `force`, `closeability_unknown` never overridable, and every refusal of the close ladder,
  answered verbatim so the console's end-confirmation flow works unchanged. **The row is written
  only after the close succeeded**; a refused or failed close writes nothing.
- **A Session with no conversation id is refused** (`archive_no_conversation`) before anything is
  closed: there is nothing to resume, so archiving it would be a close under another name.
- **The reboot record still hears of it.** The close stamps `closed_at` in the restore-after-reboot
  record ([session-restore.md](session-restore.md)), so an archived conversation is never offered
  after a reboot as well.
- **Restore resumes through the reboot restore's path** (`resumeConversation` in
  `internal/transport/http/session_restore.go`): the same opening queue, the row's directory added
  to the places the starter may resolve, and the same `Starter`. There is no second launcher.

## Old conversations are past the past list

`Starter.Resume` only opens a conversation the place's past list shows right now, and that list
stops at the newest 400 transcript files of the directory, at most 200 of them conversations
(`projects.ClaudePast`; Codex is asked for 400 threads and kept to 200). On the machine this was
written on, measured on 2026-09-27, one project directory held 2,154 Claude transcripts and another
258, so in the first a conversation archived a few weeks earlier was already off the list and an
ordinary resume would answer `not_found`.

So an archived conversation is resumed through `Starter.ResumeRecorded`, which admits it when the
assistant's own record is on disk (`projects.Recorded`: Claude's
`~/.claude/projects/<slug>/<id>.jsonl`, or Codex's rollout file under `~/.codex/sessions`), and
falls back to the past list otherwise. `TestAnArchivedConversationOffThePastListStillResumes`
writes 451 transcripts and resumes the oldest: `Resume` refuses it, `ResumeRecorded` opens it.

## Routes

| Route | Who | What |
| --- | --- | --- |
| `POST /v1/sessions/{id}/archive` | a device that may send, with an `Idempotency-Key` | body `{force?}`; answers `{ok, id, action: "archived", forced?, archived: ArchivedSession}` |
| `GET /v1/sessions/archived` | any paired device | `{sessions: [ArchivedSession], at}`, most recently archived first |
| `POST /v1/sessions/archived/restore` | a device that may send, with an `Idempotency-Key` | body `{conversations: [id…]}`; one result per distinct id: `{conversation_id, ok, code?, message?, id?, backend?, attach?}` |

`ArchivedSession` is `{conversation_id, assistant, place, place_label, cwd, title, persona?, icon?,
archived_at}`. `icon` is computed when the list is read, from `cwd`, by the same registry the
Session row draws its icon from. The contract is `api/v1/archive.schema.json`.

An archive answers every refusal a close answers, with the close's status and body — including
`close_blocked` with its `reasons` — plus `archive_no_conversation` (409), `archive_unavailable`
(503) and `archive_not_recorded` (500: the Session was closed and the row could not be written;
the answer is kept under the key, so a retry is told this rather than closing again).

A restore answers per row: `ok` with the new terminal, or `not_archived`, `already_open` (the
conversation is open in a terminal now; nothing is opened, because two assistants on one
transcript is the one thing a resume must not do), `place_unavailable`, `conversation_not_found`,
`open_failed` or `over_capacity`. Only a row that opened is removed. A list longer than the batch
bound is refused whole with `archive_batch_too_large`.

Clawdline Cloud carries the three as the relay words `archive-session` (on the Session's own
channel, as `end` is; `force` is optional), `archived-sessions` and `restore-archived`.

The bounds — 500 archived conversations and 20 per restore — are registered as
`sessions.archive_rows` and `sessions.archive_restore_batch` and listed in
[limits.md](limits.md) (N53).

## What is not covered

- **There is no forget.** A row goes when it is restored or pushed out past the bound; nothing
  removes one by hand.
- **The model is not recorded.** A restored Session starts with the assistant's default model, as
  a reboot restore does.
- **A dropped row is not announced.** Past 500 the oldest row goes and the diagnostics count says
  so; its transcript is still on disk and resumable from the place's past list while it is on it.
- **A transcript deleted since the archive cannot be restored** (`conversation_not_found`).
- **The console's Archive action and sidebar section are a separate change.** Until it lands, the
  three relay words sit in the console's `DEFERRED` list.
