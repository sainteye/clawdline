# Working in this repository

**This is the product.** Clawdline is a Go daemon plus a React console, built for macOS, Linux and
Windows. The daemon owns port 7727 on this machine. `README.md` says what it is for; `docs/` says
how each part was decided.

## The other checkout is not this one

`~/code/clawdline` is the **retired Swift app**. It was stopped on 2026-09-19, taken out of
launch-at-login, and nothing has answered its port 7717 since. On 2026-09-20 `app.clawdline.com`
also became this repository's console.

That tree is kept for two purposes, both read-only:

1. **What still has to migrate** — features this rewrite has not reached yet.
2. **How it was done the first time** — the measurements and the reasoning behind a behaviour this
   repository copies. Comments here cite it by file and line (`SessionInfo.swift:864-872`) and
   those citations are meant to stay.

**Never modify `~/code/clawdline`.** Not a file, not a commit, not a `git add`. If work you were
given seems to belong there, say so and stop; do not do it quietly. Two mechanisms read that tree
on purpose and are the only ones allowed to: `tools/check-legacy-css.sh` compares
`web/console/src/legacy/` against its `Resources/web` byte for byte, and `web/console/src/legacy/
MANIFEST.json` records where each copy came from.

A sentence in this repository that says the Swift app is running, holds a port, or is somewhere
traffic goes is false. `TestNothingSaysTheRetiredAppIsStillRunning` (`internal/config/`) fails on
one, and offers two ways to keep a measurement: write it in the past with its date, or mark the
whole file `retired-app-record` with a date.

## Where work happens

The branch is `master`. Work lands through a **disposable worktree**, never in the shared checkout:

```sh
git worktree add -f <scratch>/<name> HEAD
```

A root integrates by **merging the child's branch**, not by applying its patch: the merge commit
carries the branch, which is what makes the landing provable. Record it as soon as it lands.

A dispatched child gets its own worktree from the broker; **read its path from the task record**
(`GET /v1/orchestrator/tasks/<id>` → `.task.worktree.path`) rather than composing it, because two
brokers have two roots. `cd "$W" || exit 1` — a failed `cd` runs everything else somewhere else.

## Before anything is committed

Every one of these, and every one green:

```sh
gofmt -l internal cmd          # silent
go vet ./...
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
go test ./...
go run ./tools/contract-gen -check      # Go and TypeScript are generated together
tools/check-legacy-css.sh               # the byte-for-byte copies still match
tools/check-machine-words.sh            # no shown sentence calls the machine a Mac
tools/check-private.sh                  # nothing of the person's is in a public repo
tools/check-private.sh -history -new    # no commit behind it added one either
( cd web && npm run check && npm run build )   # when anything under web/ changed
```

A new bound — a limit, a cache size, a number of rows — must be registered in
`internal/domain/capacity`; `TestEveryBoundIsRegistered` fails otherwise. Drive the guard red
before you make it green, and keep the output that proves it went red.

## This repository is public

It is open source. **Nothing of the person's may appear in it**: no names of their businesses, no
tokens, no paths into their accounts, no content from their conversations.
`tools/check-private.sh` reads a word list kept out of git (`.git/info/private-words`); without
that list it answers 3, **undetermined**, and not 0. `-history` reads the commits as well as the
working tree, because a word committed and later removed is gone from the tree and still in what
`git push` sends — `docs/cutover.md` at `ef067d70` is the live example, and the tree scan is green
on it. A history finding names the commit, the file and the line, and never the matched text.
This history is already red and stays red: three lines at `ef067d70` are in it for good unless
somebody rewrites it, which is a decision about the whole repository and not a patch. So the
pre-commit check is `-history -new`, which prints every standing finding and is red only for one
today added. **Before this repository is made public, or pushed anywhere public,** run the whole
thing — `tools/check-private.sh -history -full -revs=--all` — and read all of it.
`docs/privacy-guard.md` is the whole of it.

Commit messages, comments and documentation are in **English**; the conversation with the person
is in Traditional Chinese.

## How a commit reads here

The subject is a sentence about what changed for a person, not a category:
`A tmux that escapes the separator is a listing, not an empty machine`. The body says what was
measured, what the machine said that contradicted the first guess, and what was deliberately left
undone. Numbers come from a run, not from memory.

## What a child does not do

- Does not `git add -A` in a shared tree, and never stages a file it was not assigned.
- **Commits its delivery to its own task branch**, and never pushes. The worktree is the child's
  own, so committing there disturbs nobody — and it is the only way the landing can be proved:
  `POST /v1/orchestrator/tasks/<id>/landing` with `state: landed` checks in Git that the branch
  carries something past its base and that the commit named on the target contains it. A child
  that leaves only a patch has delivered work the machine cannot record, and the register goes on
  saying it is owed. Measured on 2026-09-20: sixteen deliveries that had already been merged into
  master were all refused `unverified_landing / nothing_delivered`, because every brief that day
  had told the child not to commit.
- Does not start a daemon on port 7727 or write to the person's running one. Use your own
  `CLAWDLINE_NEXT_DIR` — an **empty** one, never a copy of `~/.config/clawdline-next`, whose task
  records carry real session ids and whose daemon will type into somebody's live terminal.
- Does not read the person's `~/.config/clawdline` (the retired app's own directory) except where
  the code already does, and never writes it.
- **Stops its own daemon by PID, never by pattern.** `pkill -f "clawdline serve"` matches the
  person's daemon as well as yours: it is the same command line. On 2026-09-21 that one line took
  port 7727 down for thirty-four seconds while somebody was reading their phone. Keep the PID your
  own `serve` printed and kill that.
- **Checks a restart by what it was restarted for.** `/v1/health` answers whether the daemon is
  alive, and a daemon started without `CLAWDLINE_NEXT_WEB` is alive and shows no page. On
  2026-09-21 two restarts were each confirmed with health while `/` answered 501 to everybody for
  seven and a half hours. The console is back when `GET /` answers 200, or when the line under
  `listening` in `logs/daemon.log` says `console: served from …`.


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

Where instruction files conflict, the nearest one wins, and a task brief wins over all of them.
