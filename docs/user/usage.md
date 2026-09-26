# Token use, assistant quotas and capacity

After this page you can see what a session, a dispatched task or a Board item spent its tokens on,
how much each assistant account has left, and whether anything the daemon keeps is filling up.

## A session's context and tokens

Open a session, then **⋯** → **Session 資訊** (session info). It shows the context in use against
the model's window, token use (input, output, cache read, cache write) and, when the assistant
reports them, plan limits with when they reset.

Board cards and a session's to-dos carry a **Token 帳單** (token bill) as well
([board.md](board.md)).

## What the tokens were spent on

```sh
clawdline usage                       # inside a session: that session's own bill
clawdline usage --session <conversation id>
clawdline usage --task <task id>      # a dispatched child
clawdline usage --item <item id>      # a Board item
clawdline usage --json
```

The first line has the number of calls, the peak context and the cost. Then one line per category,
most expensive first — the share, tokens and cost of, for example, `impl` (the work itself),
`delegate`, `protocol`, `board`, `rules`, `talk` and `compaction`. `rules` is always an upper bound.
Anything that could not be read is listed as a gap, never counted as zero.

Run outside a session with no flag, `usage` does not know which session you mean and exits with an
error; name one with `--session`.

## When Claude sessions compact

```sh
clawdline setting get claude_auto_compact_window
clawdline setting set claude_auto_compact_window 200000    # 50000 to 1000000 tokens
clawdline setting set claude_auto_compact_window off
```

This applies only to Claude Code sessions that Clawdline opens, from the next one it opens; Codex is
not affected. To see whether a change paid off, compare tasks by the window they ran with:

```sh
clawdline usage --compare-compaction --since 14d
```

and keep the question open on the **驗收** (Verify) page ([board.md](board.md)).

## How much each assistant has left

```sh
clawdline assistants
```

For each of Claude Code and Codex: whether it is installed, its availability (`ok`, `low`,
`exhausted` or `unknown`), when its quota window resets, and how old the reading is.

### Claude's 5h and 7d need a status line that writes them down

The two percentages at the right edge of the Status Line — `5h 96% 7d 45%` — are the account's,
not a session's, and Clawdline does not ask Anthropic for them: spending quota to find out how
much quota is left is the one cost this reading must never have.

**Claude Code hands those percentages to the stdin of whatever `statusLine.command` names in
`~/.claude/settings.json`, and to nothing else.** Not the transcript, not a file, not an endpoint
this daemon could call. With no status line configured they exist only for as long as the render
that received them, and Clawdline has nothing to read. The corner then says `方案額度 未知`
(plan limits unknown), and **Session 資訊** carries the sentence naming the file that is missing.

So the numbers appear only if your status line writes
`~/.claude/statusline-cache/rate-limits.json`. This is the ordinary state of a fresh **Linux**
install: a Mac often carries a status line over from another machine, a new Linux box rarely has
one, and the corner then looks blank rather than broken.

[claude-bestiary](https://github.com/sainteye/claude-bestiary) is one that writes it:

```sh
git clone https://github.com/sainteye/claude-bestiary.git
cd claude-bestiary && ./install.sh          # symlinks into ~/.claude, then verifies
```

then add to `~/.claude/settings.json`:

```json
{ "statusLine": { "type": "command",
                  "command": "bash ~/.claude/statusline-command.sh",
                  "refreshInterval": 2 } }
```

Any status line will do, as long as it writes that file with the `rate_limits` object Claude Code
handed it. A running session keeps the settings it started with, so the numbers appear in the
next `claude` you start. Codex needs none of this: its weekly window is read from its own
rollouts.

## Capacity: is anything filling up

Everything the daemon keeps has a limit — the log, the device list, push subscriptions, pictures,
and more. **設定** (Settings) has a **容量** (capacity) block that lists every row that needs a
look; each row reads OK, filling, nearly full, full, or not measured. It refreshes every 15
seconds.

To see a row behave as it fills, without touching your real state:

```sh
clawdline doctor capacity --drill audit.security
```

It fills that row on purpose in a throwaway directory and exits 0 only if the row warns, then fills,
then acts exactly once.

## Deeper

- [token-ledger.md](../token-ledger.md) — how the ledger reads transcripts, prices calls and
  assigns each token to a category.
- [verifications.md](../verifications.md) — judging a change by its data.
- [limits.md](../limits.md) — every bounded thing and what happens when it is full (in Chinese).
