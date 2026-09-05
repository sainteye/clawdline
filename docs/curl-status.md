# `curl` exits 0 when the server says no

`curl` reports whether it managed to speak to a server. It does not report what the server said.
A `409`, a `401`, a `503` and a `200` are all exit status 0, and a script that reads only that
status is reading a constant.

On 2026-09-05 that cost this machine two incidents in one evening, in one file:

- `build.sh` posted a restart-maintenance request, read `curl`'s exit status, and announced
  `dispatch admission is open again` for a window the server had refused with `409`. The exit
  handler then read that as permission to delete another build's note.
- The same block used `--max-time 5` on an operation measured at 146 seconds. The timeout printed
  as `restart maintenance was refused (HTTP 000)` — **a client that saw no answer, written down as
  a server that refused.**

Both are the same shape, one layer apart: *the failure looked exactly like the success*. Nobody was
careless. There was nothing on the screen to be careful about.

## The rule

> Every `curl` this repository **runs** either asks curl to fail on an HTTP error, or takes the
> status code out and compares it.

- `--fail`, `--fail-with-body`, or a short cluster carrying `-f` (`-fsSL`).
- or `-w '%{http_code}'` (`%{response_code}` also counts) with `-o` for the body, and a comparison
  on the code.

`tools/check-curl-status.py` enforces it. `test.sh` runs it in the guards phase, and
`tools/check-curl-status.py --list` prints every call it can see with its verdict, which is the
fastest way to check a change.

## What the guard can decide, and what it cannot

It can see that the code was **extracted**. It cannot see that anybody **compared** it — that needs
dataflow this does not have. So `-w '%{http_code}'` buys a pass here and still deserves a reviewer's
eye.

That limit is worth stating rather than papering over, because a check that claims more than it
verified is the defect this page is named after. What it does buy is real all the same: both
incidents above were "the status was never asked for", not "the status was asked for and ignored".

## Exemptions are written in place

A `curl` that genuinely does not care what the server answered is a real thing. `tools/shoot-assets.sh`
waits for a local static server to come up; a `404` from a page that does not exist yet still means
the listener is there, and a status check would only make the wait wrong.

```sh
# curl-status-exempt: what this waits for is a listener, and any HTTP answer is one — a 404
# from a page that is not there yet still means the server came up.
curl -s -o /dev/null -m 1 "http://127.0.0.1:$WEB_PORT/"
```

The marker goes on the call or in the few lines above it, and the reason goes after it. Not a list
of exempt line numbers somewhere else: that list goes stale silently, and it puts the reason where
the next reader is not looking. A marker with nothing behind it is a silencer, so the guard requires
a reason of at least sixteen characters and reports the ones that have none.

## Why the detection is a tokenizer and not a pattern

Every real call in this repository is three to six backslash-continued lines with quoted JSON in the
middle of it. A regular expression over that either misses the flags on the continuation lines or
reads a `-f` out of a quoted header value. And a pattern cannot tell **doing** from **mentioning**:
the word `curl` occurs 350 times in this checkout, and eleven of them are calls this repository
runs.

So the guard walks the shell with its quoting rules — `'`, `"`, `\`, `#`, heredocs, `$( )` — splits
it into commands, and asks each command that *is* `curl` what flags it was given. The enumeration
that shaped it is kept as `tools/check-curl-status.py --self-test`, twenty-nine shapes with the
negatives named: `command -v curl`, `pgrep -x curl`, the word in a comment, in a quoted string, in
an assignment's value, as a `grep` pattern, and inside all three kinds of heredoc — beside the
positives, including a `-f` inside a quoted header value that must **not** count as the flag.

## Scope: shell this repository executes, and not documentation

Scanned: tracked `*.sh` files, and tracked files with a shell shebang (`tools/git-hooks/pre-commit`).

**Not scanned: documentation.** `docs/api.md` and `docs/remote.md` between them show seventy-three
`curl` commands, every one of them written as a terminal transcript — `$ curl -s …` with the reply printed
underneath. Their reader is a person who is looking at the answer, and the answer is the point of
the page. `--fail` there would be actively wrong: it suppresses the error body, which on an API
reference is the half being explained.

The line is not "documentation is exempt", it is **who reads the result**. The one documented `curl`
that a *program* runs is in `docs/examples/process-compose.yaml`, four healthchecks a process
supervisor executes and branches on — and all four are already `curl -fsS`. That is the evidence
the boundary is in the right place rather than a convenience: where the answer is consumed by
something that cannot see it, the repository had already reached for `-f` on its own.

## Two places outside this guard's reach

Neither is fixed here, because both are outside the paths this change claimed. Both are the same
defect and are worth a task of their own:

- `Sources/OrchestratorChildBrief.swift` writes `curl -s -X POST …/progress`, `…/notify` and
  `…/complete` into **every child briefing**. An agent that posts a progress note against a stale
  secret gets a `401`, `curl -s` exits 0, and the note it believes it sent does not exist.
- `Resources/skill-guides/clawdline.md` and its `zh-TW` sibling carry the same commands for the
  same audience.
