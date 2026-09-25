# Token ledger

For whoever builds or reads the daemon's token accounting. It says what a session's tokens were
spent on — reading the board, following Clawdline's protocol, following the repository's rules,
doing the work, delegating — and adds them up per session, per child task and per Board item.

## Why this exists

Measured on 2026-09-25 over a week of this machine's transcripts (202 sessions, 26,649 API calls):

- 73% of the cost was **cache reads**: everything a session has read stays in its context and is
  re-read on every later call. A file's real cost is its size times the calls left after it was
  read, not its size.
- One session that ran for eleven days carried 29% of the week's cache reads on its own.
- Clawdline's own documents cost about 4.6% of the week; `CHILD.md` alone 1.9%. Trimming them
  (`docs/working-rules.md`, `skills/sections.go`, the child briefing) was decided from those numbers,
  and the only way to know whether the next change helps is to keep measuring.

The retired Swift app kept a usage ledger (`usage.sqlite3`, `usage_intervals`,
`/v1/orchestrator/usage/analytics`, Project and Feature attribution — `docs/usage-attribution.md`).
It answered *how much* and *for which Project/Feature*. It did not answer *on what*: how much of a
session was protocol, rules, or work. This ledger answers that first, per session, and leaves
Project/Feature attribution to a later migration of that design.

## What one API call reports

Each assistant row in a Claude Code transcript carries `message.usage`: `input_tokens`,
`cache_creation_input_tokens` (split into `cache_creation.ephemeral_1h_input_tokens` and
`ephemeral_5m_input_tokens`), `cache_read_input_tokens`, `output_tokens`. One API call is split over
several rows sharing a `message.id`; count it once. Rows with `isSidechain` belong to subagents.
Subagent transcripts live beside the session in `<session>/subagents/*.jsonl`.

Codex rollouts carry `event_msg` rows of type `token_count` whose `info.last_token_usage` is one
turn (`input_tokens`, `cached_input_tokens`, `output_tokens`, `reasoning_output_tokens`).

## Price

Per model, from `transcript.Price`: cache reads at 0.1× the input price, **1-hour cache writes at
2×** and 5-minute writes at 1.25×, output at the output price. Every Claude Code cache write seen on
this machine (45.3 M tokens over the last forty sessions) was 1-hour; the session card's existing
`transcript.Cost` counts all writes at 1.25× and so under-reports. A model with no price has no cost:
the ledger keeps tokens and says the cost is unknown, never zero.

## Attribution: what each token was spent on

The context is a stack of segments, each with a category and a size.

1. The first call's context is the resident base. The `instructions` attachment the harness records
   names the instruction files it loaded (`CLAUDE.md`, `AGENTS.md`, `MEMORY.md`); their share of the
   base is **rules**, the rest **harness**.
2. Between call *k−1* and call *k* the context grows by a measured amount:
   `ctx(k) − ctx(k−1)`, where `ctx` is input + cache writes + cache reads. That growth is call
   *k−1*'s own output plus whatever arrived — tool results, the person's text, harness attachments.
   It is divided among them by their estimated sizes, so the total stays the measured number and only
   its division is estimated.
3. Each call pays cache writes for the new segments and cache reads for the old ones, pro rata to
   their sizes, plus its output, which is charged to what the call did: the category of the tool it
   called, or **talk** when it called none.
4. A context that falls below 70% of the previous call's is a compaction: the old segments go, the
   base is kept at its proportions, and the rest is **compaction**.
5. A subagent's whole bill is **delegate**.

Every category's sum equals the session's measured total; a test holds that on every fixture.

### Categories

| Category | A tool call is this when it… |
|---|---|
| `board` | reads the Board or its assignment: `/v1/work/…`, `clawdline item`, a `root-assignments/…/ASSIGNMENT.md`, the Root Assignment first message |
| `protocol` | acts on the daemon: a curl to the daemon's port or with its token header, a `clawdline` subcommand, the `clawdline` skill, `CHILD.md`/`task.json`/`result.json`, the child's first message |
| `rules` | reads `AGENTS.md`, `CLAUDE.md`, memory files, the dispatch policy, or runs a repository guard |
| `impl` | reads, searches, edits, builds, tests, or runs git — the work |
| `delegate` | spawns or messages a subagent (`Agent`, `Task`, `Workflow`, `SendMessage`) |
| `harness` | the resident base outside the rule files, harness attachments, `ToolSearch` |
| `talk` | the person's messages, and the assistant's replies and thinking with no tool |
| `compaction` | what a compaction left above the base |
| `other` | anything else |

Decided by what the call **does**, not by what it mentions: in this repository `grep /v1/orchestrator`
over the source is work on Clawdline, not following its protocol. Sampled by hand on 2026-09-25:
`protocol` 7/7 and `board` 7/7 correct; `rules` about 4/7, because a guard run in one command with a
typecheck or a doc write takes the whole command — so `rules` is an upper bound, and says so wherever
it is shown.

What this cannot see: rules and protocol also shape how long the assistant thinks and writes. That
cost is inside `talk` and every category's output, indistinguishable by this method.

### What the reader settled

`transcript.LedgerState` (`internal/adapters/transcript/ledger.go`, `ledger_codex.go`) settled what
the rules above left open:

- **Before the first call.** What arrived before it — the person's first message, the
  `instructions` attachment, other harness attachments — is counted at its estimated size; the rest of
  the first call's context is **harness**. The `prompt_snapshot` attachment is not an arrival: it is
  the resident base itself.
- **Input.** Uncached input tokens go with the cache writes, to the new segments. A call with no new
  segment (a context a little smaller than the last) pays its writes pro rata to everything held.
- **One call doing several things.** Its output is divided evenly among its tool calls' categories.
  One shell command with several parts takes its most specific part, in the order `board`,
  `protocol`, `rules`, `impl`.
- **A tool result with no known tool use** (a background task's, say) is **other**.
- **Codex.** A `token_count` repeating the running total of the one before it is the same call and is
  counted once; one with no `info` is not a call. Input is `input_tokens` less `cached_input_tokens`,
  there are no cache writes, and the model has no price, so its cost is unknown. The
  `# AGENTS.md instructions` and `<user_instructions>` user messages Codex writes are **rules**, the
  `<environment_context>` one **harness**.
- **Which file is which.** A transcript is named by a digest of its first 4 KiB, not an inode, so the
  identity survives being copied between machines and means the same on every platform.
- **Base composition** needs the `prompt_snapshot` attachment; without it the composition is absent,
  not zero. The attachment shapes the reader expects (`systemPrompt`, `tools[].name`; files as
  objects with a `path` and a `content`) were written from the brief, not from a recorded transcript;
  a shape it does not recognise leaves that part in `other`.

## Storage and reading

- A session is read **incrementally**: the ledger keeps, per transcript, the byte offset it read to,
  the previous call's context size and output, and the segment sizes by category. A new pass reads
  from the offset. A file that shrank or changed identity is read again from the start.
- **One table, `usage_transcripts`** (`internal/adapters/store/usage.go`), one row per transcript — a
  session's own or one of its subagents' — keyed by assistant and conversation, not by path, so a
  transcript whose project directory was renamed goes on from its offset instead of counting twice.
  A row holds the path, the parent session (subagents), the child task id or Root Assignment id its
  first message names, the `LedgerState` as JSON, the per-category totals and the measured count
  with its cost (the reader prices by category; the session's cost is the sum of its categories'),
  the file's size and modification time as last read, whether that read stopped short, when it was
  read, and a reason. Totals only — no prompt, no tool input, no transcript text is stored.
- **The reading loop** (`internal/app/usage.go`, `UsageLedger.Pass`, started with the daemon's other
  background work by `StartUsage`) runs once a minute. Each pass looks for transcripts written
  within the look-back window: `~/.claude/projects/*/*.jsonl`, each session's
  `<session>/subagents/*.jsonl`, and `~/.codex/sessions/YYYY/MM/DD/*.jsonl` for the local dates the
  window covers. A row read within the window whose file the globs no longer find is stat'ed by its
  stored path, which is how a Codex rollout still written under an older date keeps being read. A
  transcript is **due** when it is new, its size, modification time or path changed, its last read
  stopped short, or it could not be read last time. Each due transcript gets one `Feed` from its
  stored state and is saved again. The home directory is the daemon's (`os.UserHomeDir`); tests give
  the ledger a temporary one.
- **Bounded**: at most `usagePassLimit` (32) transcripts are fed per pass, in round robin after the
  last one fed, so one that stays due (unreadable, or longer than a pass reads) cannot starve the
  rest; a pass that meets the limit logs it once, until a pass no longer does. The look-back window
  is `usageWindowLimit` (7 days); a transcript last written before it keeps its stored totals and is
  not visited again until it is written. Both are `docs/limits.md` N43. The bytes per `Feed` (64 MiB,
  finishing the line it is in) and the longest line decoded (8 MiB; a longer one is skipped and
  counted) are N42.
- **Failure**: a transcript that cannot be read costs that transcript its turn and nothing else; its
  row keeps the last reading's totals and says `transcript_unreadable`. A transcript read before and
  gone since keeps its totals and says `transcript_missing`, and is not asked about again. A session
  with no reading at all is `not_yet_read`, never an empty total. Each is logged once per transcript
  and reason, by conversation id, never by path; a transcript that reads fine again is forgotten, so
  its next failure is said again. What is logged is kept in memory: a restarted daemon may say each
  failure once more. A restarted daemon reads on from the stored offsets and counts nothing twice.

## Which session is which

- **A child task**: its first user message is `You are a Clawdline CHILD agent for task <id>`; the
  task record names its root (`root.session_id`).
- **A Root Assignment**: its first user message names the assignment id.
- **A Board item**: its owner Session (`OwnerSession`, and the owners in its history).
- An item's bill is its owner sessions plus the child tasks those sessions dispatched while they
  owned it. A subagent's bill is inside its parent session's `delegate`.

What the implementation settled (`UsageLedger.ForSession`, `ForTask`, `ForItem`):

- **The first message** is read once per session transcript with `transcript.FirstUser` (bounded by
  its read budget), after the transcript's first successful read; one with no person's turn yet is
  looked at again when it grows. Subagent transcripts are not looked at: they are their parent's.
- **A subagent** is its own row, linked by `parent`. A session's answer adds each subagent's whole
  measured count, cost included, to `delegate` and to the session's measured total, so the categories
  still sum to the total. Calls, peak context, compactions and the calls above 200k are the session's
  own; each subagent's calls are listed with it. A transcript that writes subagent calls inline
  (`isSidechain` rows) has them in its own `delegate` already; Claude Code writes a subagent to one
  place or the other, not both.
- **A child task** is every session transcript whose first message names it. None read yet is
  `not_yet_read` for the task.
- **A Board item's owners** are its assignment history (`work_v2_assignments`, each stint from its
  creation to its release, or to the item's close, or open) plus the current `owner_session` when no
  active assignment names it. A `new_session` assignment whose session was never written on it is
  found by the transcript whose first message names its Root Assignment. The tasks counted are those
  whose record names an owner session as `root.session_id` and that were created inside that owner's
  stint.
- **An owner session is counted whole**, once however many stints it had: the ledger is not kept by
  time, so a session that owned two items is in both bills. That is an upper bound for either item.
- **Nothing is silently left out**: every answer lists its sessions and tasks, and `gaps` names each
  session, subagent, task or Root Assignment whose reading is not current, with its reason and whether
  an earlier reading is in the totals.

## What a person and a session see

- `clawdline usage [--session <conversation> | --task <id> | --item <id>] [--json]`
  (`cmd/clawdline/usage.go`): with no flag, the calling session, named by `CLAUDE_CODE_SESSION_ID` or
  `CODEX_THREAD_ID` as `session report` names it. One header line — calls, peak context, cost, and for
  a session its calls above 200k and its compactions — then one line per category that spent
  anything, by cost (by tokens when the cost is not whole): name, share, tokens, cost, with `rules`
  marked `(upper bound)`; then one line per gap. `--json` prints the daemon's answer. A refusal prints
  `refused, <status> <code>: <message>` and exits 1, as every thin command does.
- `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>`,
  `GET /v1/usage/items/<item id>` (`internal/transport/http/usage.go`, typed in
  `api/v1/usage.schema.json`): read with a paired device or this machine's orchestrator token, as the
  Board is. Each answers a `bill`: every category in the ledger's order with its tokens by part
  (input, 1-hour and 5-minute cache writes, cache reads, output), its cost and its `share` — of the
  cost, or of the tokens when some model has no price (`share_of`). `rules` carries
  `upper_bound: true`. A session adds its own calls, peak context, compactions, the calls and the
  cost above 200k context, its base composition when the transcript recorded one, and its
  subagents; a task and an item add their sessions' calls and largest peak. Every answer carries the
  `gaps` the app layer reports.
- **What each id means when there is no reading.** A session with a row answers its row's reason
  (`transcript_missing`, `transcript_unreadable`) with the last reading's totals. A session with no
  row is `not_yet_read` when its transcript is on disk — Claude's under any project, or a Codex
  rollout — and a 404 `unknown_session` when it is not: an id that names nothing is not an empty
  bill. A subagent asked for as a session is a 404 that names its session. A task no session names
  is `not_yet_read` when the broker has its record and a 404 `unknown_task` otherwise; an item that
  is not on the Board is a 404 `unknown_item`. An id is letters, digits, `-`, `_` and `.`: anything
  else is a 400 before it reaches a query or a file name.
- **Whether it is still reading**: `usage` in `/v1/diagnostics` is the reading loop's own account —
  whether it runs, when the last pass ended and what it found (due, fed, limited, missing,
  unreadable), and `stalled` when no pass has ended for three intervals (`usageStallPasses`). It does
  not turn `ok` red: an unread ledger stops no work.
- Later: the Board card's cost line, and the Swift ledger's Project/Feature analytics migrated onto
  these rows.

## Long-running sessions

The largest lever measured is not a document but a session's age: context above 200k tokens was 42%
of all input, and cache reads grow with every call. The ledger records, per session, peak context,
calls, compactions and the cost of the calls made above a threshold, so a later change — handing a
long root over to a fresh one, or suggesting it — can be judged by what it would have saved.
