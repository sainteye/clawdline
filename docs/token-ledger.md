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

## Storage and reading

- A session is read **incrementally**: the ledger keeps, per transcript, the byte offset it read to,
  the previous call's context size and output, and the segment sizes by category. A new pass reads
  from the offset. A file that shrank or changed identity is read again from the start.
- Tables: one row per session with its assistant, transcript path, offset, reading state and the
  per-category totals (tokens by part, and cost when the model has a price). Totals only — no prompt,
  no tool input, no transcript text is stored.
- Reading is bounded: a registered limit on bytes read per pass and on sessions per pass; a line that
  does not decode is counted, not fatal; a transcript that cannot be read leaves that session
  **unknown**, never zero.

## Which session is which

- **A child task**: its first user message is `You are a Clawdline CHILD agent for task <id>`; the
  task record names its root (`root.session_id`).
- **A Root Assignment**: its first user message names the assignment id.
- **A Board item**: its owner Session (`OwnerSession`, and the owners in its history).
- An item's bill is its owner sessions plus the child tasks those sessions dispatched while they
  owned it. A subagent's bill is inside its parent session's `delegate`.

## What a person and a session see

- `clawdline usage [--session <conversation> | --item <id> | --task <id>] [--json]`: the categories
  with their share, raw token parts, and cost, one line each by default.
- `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/items/<id>`: the same, typed; a session the
  ledger could not read answers its reason (`transcript_missing`, `transcript_unreadable`,
  `not_yet_read`), never an empty total.
- Later: the Board card's cost line, and the Swift ledger's Project/Feature analytics migrated onto
  these rows.

## Long-running sessions

The largest lever measured is not a document but a session's age: context above 200k tokens was 42%
of all input, and cache reads grow with every call. The ledger records, per session, peak context,
calls, compactions and the cost of the calls made above a threshold, so a later change — handing a
long root over to a fresh one, or suggesting it — can be judged by what it would have saved.
