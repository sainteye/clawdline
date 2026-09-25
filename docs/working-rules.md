# Working rules: the long form

`AGENTS.md` is read by every session that touches this repository, and whatever it reads is re-read
on every later call of that session. So `AGENTS.md` keeps the rules and this page keeps the
incidents, measurements and procedures behind them, moved here verbatim on 2026-09-25. Read the
section a rule points to when you are about to do that thing.

## The retired Swift checkout (before 2026-09-22)

> As `AGENTS.md` read until 2026-09-22, when the Swift checkout was archived and this repository
> moved to `~/code/clawdline`. The paths below name the checkout as it was then.

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

## A task branch cut before 2026-09-22

**A task branch cut before 2026-09-22 cannot be landed, only cherry-picked.** `main` was rebased
onto the published, scrubbed history that day. A delivery branch cut from the old line carries
that line with it, so merging one makes an unfiltered history an ancestor of what gets published —
measured once at 77 findings beyond the baseline, and the pre-push hook refused it. Cherry-pick
the delivery's commits instead. The cost is that the broker cannot verify the landing: `landed`
asks for the delivery branch to be an ancestor and it never will be, so the task stays without a
truthful record and the reason belongs in the turn's report. `262c8861` is the one this was
learned on, and its record names a commit that is no longer on `main`. Branches cut after that
day have no such problem.

## Cleaning up worktrees after a landing

After recording a landing, run `tools/check-worktrees.sh`. It is a dry run and has four answers:
0 means nothing is owed, 1 means it found proved landed-and-clean residue, 2 means it could not
check, and 3 means at least one checkout is unknown. Inspect the report before running
`tools/check-worktrees.sh --apply`; apply removes only broker-owned, landed, clean checkouts and
never deletes their branches. Dirty, live, unlanded and unknown checkouts stay.

For a root's own deployment snapshot, comparison tree or other read-only throwaway checkout, use
`tools/check-worktrees.sh --ephemeral -- <command>` instead of a bare `git worktree add`. The
wrapper creates a detached checkout in a temporary directory and pairs its creation with removal.
If the command changes it or moves it off `main`, the wrapper keeps it and prints its path. See
`docs/worktrees.md` for the evidence and recovery rules.

A dispatched child gets its own worktree from the broker; **read its path from the task record**
(`GET /v1/orchestrator/tasks/<id>` → `.task.worktree.path`) rather than composing it, because two
brokers have two roots. `cd "$W" || exit 1` — a failed `cd` runs everything else somewhere else.

## Why the Windows vet is on the list

`go vet ./...` on a Mac never reads a `_windows.go` or `!unix` file, and the Windows cross-build
compiles no test, so a test that calls a POSIX-only `syscall` breaks Windows without either noticing.
The Windows vet was red on exactly that from the day it could have been run until 2026-09-21, and
a check that is always red is one nobody runs. A test that needs a Unix facility asks for it through
a per-platform file (`gone_unix_test.go` beside `gone_other_test.go`), not through `syscall` inline.

## The public repository in full

`tools/check-private.sh` reads a word list kept out of git (`.git/info/private-words`); without
that list it answers 3, **undetermined**, and not 0. `-history` reads the commits as well as the
working tree, because a word committed and later removed is gone from the tree and still in what
`git push` sends — `docs/cutover.md` at `ef067d70` is the live example, and the tree scan is green
on it. A history finding names the commit, the file and the line, and never the matched text.
This history is already red and stays red: three lines at `ef067d70` are in it for good unless
somebody rewrites it, which is a decision about the whole repository and not a patch. So the
pre-commit check is `-history -new`, which prints every standing finding and is red only for one
today added. A routine push does **not** run the whole history again: the tracked pre-push hook
compares the finding count at the published commit with the count after this push and refuses an
increase. The full `-history -full -revs=--all` audit is reserved for an initial publication or an
intentional history rewrite. `docs/privacy-guard.md` is the whole of it.

## What a child does not do, with the incidents behind it

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

