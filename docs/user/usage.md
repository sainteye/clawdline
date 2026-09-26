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
