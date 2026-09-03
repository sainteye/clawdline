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
worktree and passes silently, and the sequencer check below is worth having everywhere.

The hook reads `git rev-parse --git-dir` rather than assuming `.git`, so the sequencer check looks
at the right `MERGE_HEAD` in a linked worktree.

## What the guard refuses

Two checks, chosen to fail differently from each other.

### 1. A sequencer operation is in progress — local, **fails closed**

If `MERGE_HEAD`, `CHERRY_PICK_HEAD` or `REVERT_HEAD` exists in the git directory, or a rebase state
directory (`rebase-merge` / `rebase-apply`) does, then `git commit` concludes somebody's operation.
In a shared checkout the somebody and the committer are not reliably the same person, and the
2026-09-03 casualty was exactly this: six conflicts resolved by hand in a tree four sessions can
reach.

All four are the same hazard, and only the first was originally looked at. `git cherry-pick
--continue` and `git revert --continue` do call `pre-commit`, and so does a plain `git commit`
typed while a rebase is stopped on a conflict — so a stranger's half-finished cherry-pick used to
be yours to commit without a word.

This check reads the filesystem and nothing else. It has no dependency to be down, so there is
nothing to fail open for, and it refuses whether or not Clawdline is running. A conflict-free
`git merge` makes its commit without calling `pre-commit` at all (verified on git 2.38.1), so
reaching this check means the conflicts were resolved by hand.

### 2. A staged path is claimed by another root's live task — broker, **fails open**

Clawdline already knows who is working on what. Every dispatched task carries a `claims` array of
repository-relative paths and the identity of the root that dispatched it, and the hook reads them
from `GET /v1/orchestrator/tasks` on `127.0.0.1` with the `X-Clawdline-Orchestrator` token. A staged
path covered by a claim held by a **live** task belonging to a **different root** is refused.

Three narrowings and one widening, each of which matters:

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
- **Matched case-insensitively.** A claim is a string somebody typed into a dispatch, and this
  repository lives on a case-insensitive APFS volume where `sources/foo.swift` *is*
  `Sources/Foo.swift`. Comparing byte-for-byte let that typo through in silence. On a
  case-sensitive filesystem the same rule can over-refuse two files that genuinely differ only in
  case; the refusal says when the match was case-insensitive, and `--no-verify` is the way past it.

**Ownership is the root, not the session.** A claim is *yours* if one of this session's identities
is the task's `child.terminalId`, `child.sessionId`, `root.terminalId` or `root.sessionId`. That
means a root may commit over its own child's claims: at landing it has to, and the root that
dispatched a task is the one accountable for it. What is refused is one line of work committing
another line's.

There are two channels, because neither one is always there:

- **`CLAWDLINE_TERMINAL_ID`**, when it is set, is an explicit statement of identity, and it
  **replaces** everything below rather than joining it. A session that has said who it is must not
  additionally answer to an ambient `%N` tmux pane id, because every tmux server starts numbering
  at `%0` again and pane ids collide across servers. Nothing in the app exports this variable; it
  is for a person, a script, or the test suite to state an identity by hand.
- Otherwise the union of **`CLAUDE_CODE_SESSION_ID`**, `CLAWDLINE_SESSION_ID`, `ITERM_SESSION_ID`,
  `TERM_SESSION_ID` (both the whole `w0t12p0:<UUID>` value and the half after the colon) and
  `TMUX_PANE`. It is a union rather than a fallback chain: more identities means fewer false
  refusals, and a false refusal is the failure this design cannot afford.

**`CLAUDE_CODE_SESSION_ID` is the one that decides a landing**, and it is worth saying why. Claude
Code sets it in every session, and its value is exactly what Clawdline records as `child.sessionId`
and what a root passes as `root.sessionId` when it dispatches. `root.terminalId` looks like the
obvious field and is not a stored one: `Orchestrator.record(of:)` recomputes it from the
SessionWatch inventory on every response and omits the key entirely when that lookup misses — and
that inventory is the component this repository has already recorded as needing ten minutes to
rebind after a crash. A guard reading terminals alone therefore refused a root landing its own
child's work for as long as the inventory was cold, and refused it with a message that named the
committer's own root as the owner in the same breath. `root.sessionId` is always on the record.

**A session this guard cannot identify is warned, not refused.** If none of those variables is set
— a script, a cron job, a terminal that exports nothing — or if the task holding the claim carries
no session or terminal of its own, then the hook cannot tell whether the claim is the committer's.
That is the *identity* half of the broker-dependent check failing, so it takes that check's
direction: it prints the loud warning, names the claim and the task holding it, says how to state
an identity next time, and allows the commit. The alternative is a refusal aimed at somebody the
hook cannot see, which is how a guard whose one false refusal lands on the legitimate landing gets
uninstalled by lunchtime.

## The failure mode, and why

**The claims check fails open, loudly.** Clawdline is not always running, and a hook that refuses
every commit while the app is down does not survive the morning: the first thing anybody does with
it is uninstall it, and an uninstalled hook guards nothing. So whenever it loses its answer, the
hook prints a warning that says in as many words that **this commit was NOT checked**, lists the
staged paths it cannot vouch for, and exits 0.

**Every way of losing that answer says which one it was.** They used to share one sentence —
`Clawdline is not answering` — and two of them were lies. An orchestrator token that has been
rotated produces an HTTP 401 from an app that is answering perfectly well, and the person reading
that warning goes to check whether the app is running while every commit in between passes
unchecked. So:

| What happened | What the warning says |
| --- | --- |
| nothing is listening, or the broker is too slow | `Clawdline is not answering at <url> (<reason>)` |
| an HTTP error status | `Clawdline answered at <url> with HTTP <code> <reason>` — and for 401/403, that the token this hook read is not the one Clawdline expects |
| a body that is not JSON | `Clawdline answered at <url> with a body this hook could not read as JSON` |
| valid JSON that is not a task list | `Clawdline answered at <url> in a shape this hook does not understand` |
| the token file cannot be read, or is empty | the path it tried, and which of the two it was |
| no `python3` | that, and that the commit was not checked |

All six are exercised by `Tests/git-hooks.mjs`; three of them used to be claims about untested
code paths.

**The sequencer check fails closed**, because it depends on nothing.

**Identity failure takes the claims check's direction, not the sequencer check's** — see *A session
this guard cannot identify is warned, not refused* above. It is a broker-dependent answer: it rests
on what Clawdline recorded about a session, so when it is missing the hook warns rather than
refuses.

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

**`git commit --amend` carries what is already in HEAD through unchecked.** An amend is read
against the commit it replaces, so a foreign path that commit already held does not appear in the
diff the hook sees. This was left open on purpose rather than closed: `pre-commit` is handed no
arguments and runs before `prepare-commit-msg`, so the only way to know an amend from an ordinary
commit is to read the parent process's `argv` — a guess that is wrong in both directions, and one
that would put a heuristic in the one check that has to be trustworthy. The hole makes no new
damage either, because that path was already in that commit; a path the amend *newly* stages is
refused like any other. What it means in practice is narrow and worth knowing: something swept in
by `--no-verify` or by a conflict-free merge is not looked at again by a later amend.

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

**Both directions have a control group.** That stub is the control for every refusal. The control
for every *allow* is the request log: a commit the hook let through has to have asked the broker
exactly once first, or the allow proves nothing that a hook which had stopped running would not
also produce. Each ownership rule that allows is additionally paired with the same scenario, one
identity changed, which has to refuse — identity is the most intricate part of this guard, and it
was the part with the thinnest assertions.

The file also runs *itself* once, in an abort mode, to prove that an early stop takes its sandbox
and its stand-in broker with it. `stop()` exits the process and `process.exit()` skips `finally`,
so every early abort used to leave a `mkdtemp` directory and a live node process behind.
