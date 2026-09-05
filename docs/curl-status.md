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
`tools/check-curl-status.py --list` prints every call it can see with its verdict and which of the
two scopes below it came from, which is the fastest way to check a change.

The same rule reaches the text this repository hands to *agents* — child briefings and the shipped
skill guides — for the same reason and with one difference: there the remedy is `--fail-with-body`,
because the reader is told to branch on the error body. "Scope: who reads the result" below has it.

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

## Scope: who reads the result

Two things are scanned, and the line between them and everything else is not "code yes,
documentation no". It is **who reads what the server said**.

**Shell this repository executes.** Tracked `*.sh` files, and tracked files with a shell shebang
(`tools/git-hooks/pre-commit`). Nobody is watching when these run; a branch reads the status or
nothing does.

**Instructions this repository hands to an agent, which the agent then runs.** The string literals
of `Sources/**/*.swift` — every child briefing is written there — and `Resources/skill-guides/*.md`,
which is copied into the app bundle and therefore installs with Clawdline on every machine, not
only this one. A briefing has exactly one reader, it is not a person, and it types what it is
shown. So a `curl` there is a call with an agent's hands on it, and the failure it cannot see is
the same failure `build.sh` could not see.

**Not scanned: documentation.** `docs/api.md` and `docs/remote.md` between them show seventy-three
`curl` commands, every one of them written as a terminal transcript — `$ curl -s …` with the reply printed
underneath. Their reader is a person who is looking at the answer, and the answer is the point of
the page. `--fail` there would be actively wrong: it suppresses the error body, which on an API
reference is the half being explained.

The one documented `curl` that a *program* runs is in `docs/examples/process-compose.yaml`, four
healthchecks a process supervisor executes and branches on — and all four are already `curl -fsS`.
That is the evidence the boundary is in the right place rather than a convenience: where the answer
is consumed by something that cannot see it, the repository had already reached for `-f` on its own.

## What the instructions were fixed to say

`--fail-with-body`, not `--fail`. Both make the command fail; only one keeps the body, and the body
is the point — every one of these pages tells its reader to branch on the typed `code` inside it.
Suppressing it to make the command fail loudly would trade one silence for another.

Measured against this machine's own broker on 2026-09-05, posting a progress note with a wrong
secret:

```
curl -s …                   {"error":{"code":"forbidden", …}}                     exit 0
curl --fail-with-body -sS … curl: (22) The requested URL returned error: 403
                            {"error":{"code":"forbidden", …}}                     exit 22
```

**And the flag is only half of it.** The briefing already told a child what to do when `curl`
*cannot connect* — the failure curl reports in its exit code, exit 7, on a sandbox with no loopback
— and said nothing about the failure it does not report. So each of the four recipes now carries
what a refusal means where it stands: the progress note was not recorded, the push did not happen,
a completion announce that failed changes nothing because the file already reported the work, and
an `inflight` that failed is not an empty board. The guides say it once, in §1, next to the first
command.

## Telling a command from an illustration of one

In shell, "doing" and "mentioning" are told apart by tokenizing. In markdown and in a Swift literal
the same question has a different answer, and it is structural rather than lexical: **a command an
agent runs is written as code.** Each file is projected into the shell it actually contains, blank
everywhere else, and the same tokenizer reads the projection:

- fenced blocks tagged `bash`, `sh`, `shell` or `zsh` — a ```` ```json ```` block is what came back,
  and an untagged fence is how this repository shows output;
- inline code spans, because a command is sometimes written in the middle of a sentence — and the
  completion announce in the briefing is three lines inside one pair of backticks;
- Swift string literals, with `//`, `///` and `/* */` dropped: all six `curl` lines in
  `Sources/Orchestrator.swift` are comments, and none of them is a command.

Every newline survives the projection, so a finding names the line a person will open. Two details
are worth writing down because getting either wrong changes an answer:

- **An interpolation is a value.** `\(task.id)` is filled with a run of `x`, because leaving the
  parentheses would end the command at the `)` — the tokenizer treats one as a boundary — and every
  flag written after the URL would be invisible.
- **A backtick becomes a `;`.** Blanking them instead joined the two mentions in "plain `curl`
  exits 0 … a `401` and a `200` are the same exit status" into the single command `curl 401`, and
  the guard reported the sentence explaining the defect as an instance of it.

`curl` with nothing after it is a program's name — `edits` gets past `cat` / `mkdir` / `curl` —
rather than a call. An illustration that does carry arguments is indistinguishable from a command
by any rule this guard could hold, and it is resolved the way the rest of this page resolves
ambiguity: in place, with a reason.

```md
<!-- curl-status-exempt: the point of this example is the error body a --fail would hide -->
```

Two shapes are deliberately not read as commands, and both are named here rather than left to be
discovered: a fenced block whose lines start with a `$ ` prompt (that is a transcript, and the
tokenizer sees a command called `$`), and a single-line Swift literal, which carries no code block.
A shell command the app builds as a Swift string to *execute* would belong to the first rule on this
page rather than this one; there is none in the tree today.

## Still outside this guard's reach

Same audience, outside the paths this change claimed, and each carrying one unchecked call:
`AGENTS.md` and `skills/clawdline/SKILL.md` with its `zh-TW` sibling. They are listed in
`INSTRUCTION_NOT_YET` in the guard, so whoever brings them into `INSTRUCTION_SOURCES` fixes their
calls in the same commit.
