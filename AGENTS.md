# Working in this repository

**This is the product.** Clawdline is a Go daemon plus a React console, built for macOS, Linux and
Windows. The daemon owns port 7727 on this machine. `README.md` says what it is for; `docs/` says
how each part was decided. This page holds the rules; `docs/working-rules.md` holds the incidents
and procedures behind them — read its section when you reach the thing it covers.

## The retired Swift app

The Swift app was stopped on 2026-09-19 and archived on 2026-09-22 as bundles under
`~/code/clawdline-archive-20260922`; this checkout now lives at `~/code/clawdline`. Its code is a
record of what still has to migrate and how it was done the first time. Comments cite it by file and
line (`SessionInfo.swift:864-872`) and those citations stay. Never modify the archive.

A sentence here that says the Swift app is running, holds a port, or is somewhere traffic goes is
false; `TestNothingSaysTheRetiredAppIsStillRunning` (`internal/config/`) fails on one. Write such a
measurement in the past with its date, or mark the whole file `retired-app-record` with a date.

## Where work happens

The branch is `main` (it was `master` until 2026-09-22). Work lands through a **disposable
worktree**, never in the shared checkout:

```sh
git worktree add -f <scratch>/<name> HEAD
```

- A root integrates by **merging the child's branch**, not by applying its patch, and records the
  landing as soon as it lands. A branch cut before 2026-09-22 can only be cherry-picked
  (`docs/working-rules.md`).
- After recording a landing, run `tools/check-worktrees.sh` (a dry run: 0 nothing owed, 1 landed
  residue, 2 could not check, 3 unknown) and inspect it before `--apply`. For a read-only
  throwaway checkout use `tools/check-worktrees.sh --ephemeral -- <command>`. `docs/worktrees.md`.
- A dispatched child's worktree path comes from the task record
  (`GET /v1/orchestrator/tasks/<id>` → `.task.worktree.path`), never composed.
  `cd "$W" || exit 1` — a failed `cd` runs everything else somewhere else.

## Before anything is committed

Every one of these, and every one green:

```sh
gofmt -l internal cmd          # silent
go vet ./...
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
GOOS=windows go vet ./...      # the Windows-only files, and every test, as Windows compiles them
go test ./...
go run ./tools/contract-gen -check      # Go and TypeScript are generated together
tools/check-legacy-css.sh               # the byte-for-byte copies still match
tools/check-machine-words.sh            # no shown sentence calls the machine a Mac
tools/check-private.sh                  # nothing of the person's is in a public repo
tools/check-private.sh -history -new    # no commit behind it added one either
( cd web && npm run check && npm run build )   # when anything under web/ changed
```

- A test that needs a Unix facility asks for it through a per-platform file
  (`gone_unix_test.go` beside `gone_other_test.go`), not through `syscall` inline.
- A new bound — a limit, a cache size, a number of rows — must be registered in
  `internal/domain/capacity`; `TestEveryBoundIsRegistered` fails otherwise. Drive the guard red
  before you make it green, and keep the output that proves it went red.
- When slow sends, long loading, pending messages that never clear or lost events recur, do not
  only add a timeout or a spinner: trace the whole capacity and protocol path — queue and
  concurrency limits, backpressure, synchronous external dependencies, retry amplification,
  idempotency and receipts, SSE resume, stale snapshots, failure isolation — and tell apart
  accepted, executed, delivered, observed and acknowledged. The fix carries typed errors and
  failure-injection tests.

## This repository is public

**Nothing of the person's may appear in it**: no names of their businesses, no tokens, no paths
into their accounts, no content from their conversations. `tools/check-private.sh` without its word
list answers 3, undetermined, not 0; the pre-commit check is `-history -new`, and a history finding
names the commit, file and line, never the matched text. `docs/privacy-guard.md` is the whole of it.

Commit messages, comments and documentation are in **English**; the conversation with the person
is in Traditional Chinese.

## How a commit reads here

The subject is a sentence about what changed for a person, not a category:
`A tmux that escapes the separator is a listing, not an empty machine`. The body says what was
measured, what the machine said that contradicted the first guess, and what was deliberately left
undone. Numbers come from a run, not from memory.

## How every delivery report ends

The final response for every task ends with exactly two unbulleted status lines, in this order,
with no text, heading, code fence or other content after them. Choose the GitHub line and the
Build/Cloud line independently from the evidence for the current delivery.

Use exactly one of these GitHub lines:

```text
✅ 已經 commit, push 更新到 github
🕰️ 還未 commit, push
```

The GitHub line is complete only when the delivery is committed and that commit is reachable from
the current `origin/main`. A local commit, an unpushed merge or a pending/rejected push uses the
pending line.

Use exactly one of these Build/Cloud lines:

```text
✅ 已經更新 build & Cloud 到最新版本
🕰️ 還未更新 build & Cloud 到最新版本
```

The Build/Cloud line is complete only when production `BUILD.json` names the delivered commit and
the served main bundle passes the `CloudGate` check from `docs/hosted-console.md`. A queued build,
a preview deployment, a stale production stamp or a skipped verification uses the pending line.

Any required review-residue statement, including `Fixed but not yet released (awaiting review)`,
goes before these two lines so the status pair always remains the final two lines of the response.

## What a child does not do

- `git add -A` in a shared tree, or stage a file it was not assigned.
- Leave its delivery uncommitted: it **commits to its own task branch** and never pushes — the
  commit is what lets the landing be proved.
- Start a daemon on port 7727 or write to the person's running one. Use an **empty**
  `CLAWDLINE_NEXT_DIR` of its own, never a copy of `~/.config/clawdline-next`.
- Read `~/.config/clawdline` except where the code already does, or write it.
- Stop a daemon by pattern: stop your own **by the PID** your `serve` printed.
- Confirm a restart with `/v1/health` alone: the console is back when `GET /` answers 200.

The incident behind each of these is in `docs/working-rules.md`.

## The single pages

| Question | Page |
|---|---|
| What is the architecture | `docs/architecture.md` |
| Why a decision is what it is | `docs/design-decisions.md` |
| How work is tracked: board, to-dos, Backlog, issues | `docs/work-system.md` |
| What each platform does and does not do | `docs/cross-platform.md`, `docs/linux.md` |
| The broker: dispatch, landing, handoff | `docs/broker.md` |
| Cloud and the phone | `docs/remote.md`, `docs/cloud-wire.md` |
| Deploying app.clawdline.com | `docs/hosted-console.md` |
| Every bound and where it is enforced | `docs/limits.md` |
| What keeps the person out of a public repository | `docs/privacy-guard.md` |
| The incidents and procedures behind these rules | `docs/working-rules.md` |

Where instruction files conflict, the nearest one wins, and a task brief wins over all of them.
