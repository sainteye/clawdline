# The shared-tree commit guard

Several agent sessions share `/Users/sainteye/code/clawdline`, and sharing a checkout means sharing
one git index. [`AGENTS.md`](../AGENTS.md) has said for months what follows from that: stage by
name, read `git diff --cached --stat` before committing, and look for *a path you did not stage
yourself*. The rule is correct and it has been broken three times:

- **2026-08-28, twice.** A plain `git commit` with no pathspec swept in files another session had
  already `git add`-ed into the shared index.
- **2026-09-03 03:30:01.** A bare `git reset --hard HEAD` destroyed another line's in-progress
  merge: `MERGE_HEAD`, six hand-resolved conflicts, and one new file of 2,265 lines.

All three times the written rule existed, the check was run, the output was correct, and it was read
past. So the answer is not more prose. It is this:

> **A commit must not contain a path the committing session did not stage itself** — and something
> other than a person's attention has to be the thing that says so.

## Install it

```sh
sh tools/install-git-hooks.sh
```

It points `core.hooksPath` at the tracked directory `tools/git-hooks`, so the hook lives in git and
a fresh clone gets it by running one command rather than by copying a file into `.git/hooks`. Run it
as often as you like; installing twice prints `already installed` and changes nothing. If
`core.hooksPath` is already set to something else, the installer **reports that value and stops**.
It will not take a setting that belongs to somebody else — that is the same class of mistake the
hook exists to prevent.

**`core.hooksPath` is repository-wide.** A linked worktree shares the repository's config, so
running the installer from inside a Clawdline task worktree installs the hook for the main checkout
and for every other worktree at the same moment. Install it deliberately, from the checkout itself,
rather than as a side effect of a task. Once installed, worktrees are covered too, which is
harmless: in an isolated worktree the claims check finds no task whose `projectDir` is that
worktree and passes silently, and the merge check below is worth having everywhere.

The hook reads `git rev-parse --git-dir` rather than assuming `.git`, so the merge check looks at
the right `MERGE_HEAD` in a linked worktree.

## What the guard refuses

Two checks, chosen to fail differently from each other.

### 1. A merge is in progress — local, **fails closed**

If `.git/MERGE_HEAD` exists, `git commit` concludes somebody's merge. In a shared checkout the
somebody and the committer are not reliably the same person, and the 2026-09-03 casualty was exactly
this: six conflicts resolved by hand in a tree four sessions can reach.

This check reads one file. It has no dependency to be down, so there is nothing to fail open for,
and it refuses whether or not Clawdline is running. A conflict-free `git merge` makes its commit
without calling `pre-commit` at all (verified on git 2.38.1), so reaching this check means the
conflicts were resolved by hand.

### 2. A staged path is claimed by another root's live task — broker, **fails open**

Clawdline already knows who is working on what. Every dispatched task carries a `claims` array of
repository-relative paths and the identity of the root that dispatched it, and the hook reads them
from `GET /v1/orchestrator/tasks` on `127.0.0.1` with the `X-Clawdline-Orchestrator` token. A staged
path covered by a claim held by a **live** task belonging to a **different root** is refused.

Three narrowings, each of which matters:

- **Live only.** A task is live when it has no `finishedAt` *and* its state is not one of
  `success`, `failure`, `cancelled`, `timeout`, `spawn_failed`, `expired`, `abandoned`,
  `superseded`. Two signals have to agree, so a state name the hook has never heard of is not
  silently treated as over.
- **This repository only.** The task's `projectDir` must resolve to the top of the checkout being
  committed into.
- **Not isolated.** Worktree-isolated tasks are skipped. The broker already empties their claims —
  `Orchestrator.prepareClaimsForIsolation` returns `claims_ignored_for_worktree`, because a claim
  names the shared checkout and an isolated child cannot touch it at that spelling — and honouring
  them anyway would refuse the root's own landing commit, which stages precisely the paths its child
  claimed. A guard that refuses landings is a guard that gets uninstalled.

**Ownership is the root, not the session.** A claim is *yours* if this terminal is the task's
`child.terminalId` or its `root.terminalId`. That means a root may commit over its own child's
claims: at landing it has to, and the root that dispatched a task is the one accountable for it.
What is refused is one line of work committing another line's. The terminal is read from
`CLAWDLINE_TERMINAL_ID`, else `ITERM_SESSION_ID` / `TERM_SESSION_ID` (both the whole
`w0t12p0:<UUID>` value and the half after the colon), else `TMUX_PANE`. A session the broker has
never heard of owns nothing, so every live claim is foreign to it — which is the right answer for a
person typing into the shared tree by hand.

## The failure mode, and why

**The claims check fails open, loudly.** Clawdline is not always running, and a hook that refuses
every commit while the app is down does not survive the morning: the first thing anybody does with
it is uninstall it, and an uninstalled hook guards nothing. So when the broker does not answer, the
token cannot be read, the answer arrives in an unexpected shape, or `python3` is missing, the hook
prints a warning that says in as many words that **this commit was NOT checked**, lists the staged
paths it cannot vouch for, and exits 0.

**The merge check fails closed**, because it depends on nothing.

That split is the whole design decision. Coverage is not the scarce thing here — a guard that is
still installed next month is.

## The escape hatch

Every refusal names it:

```sh
git commit --no-verify
```

A guard with no way past it is a guard people route around permanently. The remedy the refusal
recommends first is the repository's own, from `AGENTS.md`:

```sh
git reset -- <their path>      # unstages without touching their bytes
git commit -m "<message>"      # then commit your reviewed index
```

The refusal deliberately does **not** recommend `git commit -- <your paths>` as the general fix.
That commits the *worktree* content of the named paths rather than the staged hunks, so it can
absorb another session's unstaged work instead — the opposite trap, and also documented in
`AGENTS.md`. It is safe only when the whole worktree diff of every named path is yours. (A partial
commit is read through git's temporary index, so when you do use one the hook sees only the paths
you named and stays quiet.)

## The honest scope

**A `pre-commit` hook runs on `git commit`, and on nothing else.**
It **does not run on `git reset --hard`**, nor on `git checkout -- <path>`, nor on `git stash`, and
a conflict-free `git merge` writes its commit without calling it.

So this guard could have stopped the two 2026-08-28 incidents, and the 2026-09-03 03:30 incident
**could not have** been stopped by it. Nothing in the hook or the installer claims otherwise, and
neither should anything built on top of it.

There is also no local per-path attribution. Git records *what* is staged, never *who* staged it, so
with the broker unreachable there is nothing in `.git` that can tell your `git add` from somebody
else's. That is why the claims check is the mechanism and why its outage is a warning rather than a
refusal.

## Two mechanisms looked at and not built

Both were measured rather than remembered; the transcript of the runs is in the task's
`artifacts/report.md`.

**A `reference-transaction` hook does not save you from `git reset --hard`.** It *does* fire —
contrary to the assumption this work started with — and it *can* abort the transaction. On git
2.38.1, with a modified `a.txt` and a staged `precious.txt`, a hook exiting non-zero on `prepared`
produced `fatal: ref updates aborted by hook` and exit 128 — and `precious.txt` was already gone and
`a.txt` already reverted. **The working tree and the index are destroyed before the ref transaction
runs**, and for `git reset --hard HEAD` the ref update is a no-op (old and new are the same commit)
that there was never any point refusing. Covering `reset --hard` needs something else entirely; it
is a separate decision and not this one.

**A `post-index-change` hook could give real local attribution, and was left alone.** It fires on
`git add` (verified on the same git), so a companion hook could append `(path, terminal, time)` to a
ledger under `.git/` and `pre-commit` could then attribute every staged path without the broker.
That is strictly stronger than the claims check where it applies. It was not built here because it
adds a hook that runs on **every** index write in a tree four sessions are working in — including
inside `git checkout`, `git merge` and `git commit` itself, with a recursion hazard if the ledger
writer runs a git command that refreshes the index — and because a ledger that is missing entries
(anything staged before the hook was installed) has to choose between refusing unattributed paths
and ignoring them. That is a design decision with its own failure mode, and it belongs to whoever
takes it, not to this change.

## The test

`Tests/git-hooks.mjs`, run by `./test.sh`. It builds throwaway repositories under `mkdtemp`, serves
a stand-in broker from node, and drives real `git commit` runs against them. It never touches the
shared checkout: `HOME`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` and `GIT_CEILING_DIRECTORIES` all
point into the sandbox, every git process goes through one helper that refuses a working directory
outside it, and the last two checks prove that containment is real by pointing the helper at this
checkout and requiring it to refuse.

Its final check is the one that keeps the rest honest: the refusal scenario is replayed against a
stubbed hook that always exits 0, and the commit has to succeed. If it were refused there, something
other than the hook was doing the refusing and every check above it would be worthless.
