# Worktree lifecycle

For task scratch, Root Assignment data, and Go build caches, see
[disk lifecycle](disk-lifecycle.md). Their ownership rules differ from a
worktree's.

Worktrees are disposable only after the system can prove what they hold, who owns them, and where
their delivery landed. Age, a UUID-shaped directory, a branch prefix, and a clean-looking path are
not that proof. `git worktree prune` is also not a lifecycle: it removes stale metadata only after
the checkout directory has already gone.

`tools/check-worktrees.sh` is the repository guard. It reads Git's registered worktrees and the
current broker inventory for this repository. The primary checkout is reported but is outside the
cleanup scope. Every linked checkout receives exactly one of these answers:

| Classification | Required evidence | Action |
|---|---|---|
| `landed_clean` | The broker inventory names the exact path and branch as `droppable`, which already requires a settled landing and an absent owner; or the checkout is a Root checkout (below) whose owner record names it, whose `HEAD` is on the target and inside which `lsof` lists no working process. Git also reports no tracked or untracked changes. | Dry run reports it. `--apply` checks Git again, then asks `git worktree remove` without `--force`. The branch remains. |
| `settled_clean` | The broker inventory names the exact path and branch as `droppable` with `settlement` `incorporated`, `nothing_to_land` or `abandoned`: the delivery was finished without its own branch being merged. The inventory offers it only when the branch has no commit past its base (`nothing_to_land`, `abandoned`) or nothing past the delivery head an incorporation proved while the carrier's landing still stands on the target (`incorporated`), and the owner is absent. Git also reports no tracked or untracked changes. | Same as `landed_clean`: dry run reports it, `--apply` checks Git again and removes the checkout without `--force`. The branch remains. An abandoned checkout with commits is not offered and stays `unknown`. |
| `unlanded` | The broker inventory associates the branch with a task whose landing is unsettled, or a Root checkout's `HEAD` is not on the target; Git reports a clean checkout. | Keep it. Land or abandon the task first. |
| `uncommitted` | Git reports tracked or untracked content different from `HEAD`. | Keep it. Preserve the content as a verifiable patch or branch before any later cleanup. |
| `active_task` | The broker inventory associates the branch with a non-terminal task, or a process works inside a Root checkout. | Keep it, whether or not it currently looks clean. |
| `unknown` | Ownership, status, or broker evidence is absent or unreadable; the checkout is not an exact path-and-branch match; or a nested repository/content filter makes Git status incomplete. | Keep it. Restore the missing evidence or have its owner dispose of it. |

The exact broker task record is ownership evidence, and so is the owner record that
`--root-checkout` writes when it creates a checkout. A location under a worktree root alone is not.
This is why a checkout made by another session, including one under a familiar temporary directory,
stays `unknown` unless the broker associates it with a task or its creation record names it.

The Projects worktree page offers **Delete worktree** on every checkout except the repository's main
one. A person who presses it means it: the machine does not veto an unlanded, dirty, locked or
in-use checkout. When the row is not reported landed and clean, the dialog says that uncommitted
files and a Session's work there will be lost, and the button stays available. The DELETE route
observes the repository again only to resolve the id to a registered path; it refuses the main
checkout, then runs `git worktree remove --force --force`, which leaves the branch and its commits
intact. The browser can request this through Clawdline Cloud. The classifications above still
govern every automatic cleanup, including `tools/check-worktrees.sh --apply`.

## Commands and exit status

The default is a dry run:

```sh
tools/check-worktrees.sh
tools/check-worktrees.sh --apply
tools/check-worktrees.sh --target <branch>    # default main
```

`--target` names the branch a Root checkout's `HEAD` must be on to count as landed; it is also the
branch `--ephemeral` checks before removing its checkout.

Audit mode follows the repository's four-answer convention:

| Exit | Meaning |
|---:|---|
| 0 | The evidence was complete and there is no remaining removal candidate. |
| 1 | The dry run found one or more proved removal candidates. Nothing moved. |
| 2 | Git, the repository, or an explicitly supplied evidence file could not be read. |
| 3 | At least one linked checkout is `unknown`, or a final apply guard could no longer decide. Proven candidates may have been removed, but unknown paths were not. |

An unavailable daemon is not reported as an empty inventory. The script still reports locally
visible dirty content, classifies the remaining broker-owned and unowned checkouts as `unknown`,
and exits 3. A Root checkout is proved by its owner record, not by the broker, so a clean, idle
one on the target is still `landed_clean` and `--apply` removes it before exiting 3. `--apply` never turns missing
broker evidence into permission. It never runs `git branch -d`, `git branch -D`, or an equivalent;
removing a worktree leaves its branch and commits in the repository.

Run the dry check after each landing is recorded. This timing matters: before the landing record,
the system must say `unlanded`; after it, the broker can prove both the target and the absent child
owner. Apply every proved `landed_clean` and `settled_clean` result after reading the dry run. Remove a root-owned task
checkout when its commit is on `main`, it is clean, and its owner is done with it. Report the count
removed and the reason for each candidate kept, even if no checkout was removed. The daemon's own
periodic reclamation remains the backstop for broker-owned task trees. The repository guard is the
visible receipt at the root's integration boundary.

## Root-owned temporary trees

Deployment snapshots and comparison trees should not enter the audit backlog at all. Pair their
creation and disposal in one process:

```sh
live_app="$(git rev-parse --show-toplevel)/dist/Clawdline Next.app"
tools/check-worktrees.sh --ephemeral -- ./tools/package-macos.sh --install-to "$live_app"
tools/check-worktrees.sh --ephemeral --rev HEAD~1 -- sh -c 'some read-only comparison'
```

The explicit install target matters: without it, `package-macos.sh` installs the new bundle inside
the throwaway worktree while the app serving port 7727 keeps running from the primary checkout.
Check the served `/BUILD.json` after the restart, not only `/v1/health`.

The wrapper creates a detached worktree below the operating system's temporary directory, runs the
command from that checkout, and removes it on exit only when it is clean and its `HEAD` is contained
in `main`. It does not use `--force`. If the command writes uncommitted content, moves to an
unlanded commit, or Git refuses removal, the wrapper prints the retained path. The command's exit
status is preserved. This creation-time pairing is preferred to asking a later sweep to guess who
owned an anonymous scratch checkout.

A Root that needs a checkout for longer than one command — a comparison, a test copy, a branch it
will merge later — creates it with an owner record instead of under `/tmp`:

```sh
tools/check-worktrees.sh --root-checkout compare-bots          # detached at HEAD
tools/check-worktrees.sh --root-checkout before-fix --rev HEAD~3
```

The checkout is `$CLAWDLINE_NEXT_DIR/root-worktrees/<name>` (override the directory with
`CLAWDLINE_ROOT_WORKTREES`), and `<name>.owner.json` beside it records the repository, real path,
base commit, creation time and `CLAWDLINE_SESSION_ID` or `TMUX_PANE`. The name is letters, digits,
`.`, `_` and `-`, and may not start with `.`; an existing name is refused. Switch to a branch inside it if the work will be
merged. The audit then reports it with owner `root:<session>` and never as `unknown`: dirty is
`uncommitted`, a commit not on the target is `unlanded`, a working process inside is `active_task`,
and only a clean, landed, idle checkout is `landed_clean`. `--apply` checks the record, target and
process list again, runs `git worktree remove` without `--force` and then removes the record. A
missing or mismatched record makes the checkout `unknown` again; it is never adopted by location.

Copies made with `cp -R` or `rsync` are not Git worktrees and no audit can prove them; make the
checkout this way instead. Existing directories in `/tmp` are not adopted or removed by this tool.

## Web dependencies in a linked worktree

`npm test`, `npm run check`, `npm run build`, `npm run build:cloud`, `npm run dev`, and the console
workspace's build, `build:cloud`, `build:demo`, dev, and preview commands run `tools/ensure-web-deps.mjs` before their real command. There is no manual worktree setup.
`(cd web && npm run deps)` runs the same preparation explicitly.

When the primary checkout has a valid install for the same `package-lock.json`, the preparer makes a
real `web/node_modules` directory in the linked checkout. Its external package entries point to the
primary checkout, while every npm workspace entry points to the current checkout. Linking the whole
`node_modules` directory is deliberately unsafe: npm's relative workspace links would then resolve
`@clawdline/core`, `@clawdline/contract`, and `@clawdline/console` from another branch.

If the lockfiles differ, or the primary install is absent or invalid, the preparer runs
`npm ci --ignore-scripts` in a staging directory and atomically publishes an isolated dependency
tree. Repeating the command keeps the ready tree. A changed lockfile replaces only a tree carrying
the preparer's marker; a physical install created directly by a person or npm is left alone.

## Recovery

For `uncommitted`, first snapshot the complete non-ignored tree and prove that the saved patch or
preservation branch reconstructs it; the daemon reclaimer's implementation in
`internal/app/orchestrator/reclaim.go` is the reference. For `unlanded`, record a verified landing,
an explicit abandonment, or `nothing_to_land` as appropriate. For `active_task`, wait for the task
to reach a terminal state and for its owner to leave. For `unknown`, repair the broker or Git
evidence; do not move the directory to make the warning disappear.
