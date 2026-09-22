# Worktree lifecycle

Worktrees are disposable only after the system can prove what they hold, who owns them, and where
their delivery landed. Age, a UUID-shaped directory, a branch prefix, and a clean-looking path are
not that proof. `git worktree prune` is also not a lifecycle: it removes stale metadata only after
the checkout directory has already gone.

`tools/check-worktrees.sh` is the repository guard. It reads Git's registered worktrees and the
current broker inventory for this repository. The primary checkout is reported but is outside the
cleanup scope. Every linked checkout receives exactly one of these answers:

| Classification | Required evidence | Action |
|---|---|---|
| `landed_clean` | The broker inventory names the exact path and branch as `droppable`, which already requires a settled landing and an absent owner; Git also reports no tracked or untracked changes. | Dry run reports it. `--apply` checks Git again, then asks `git worktree remove` without `--force`. The branch remains. |
| `unlanded` | The broker inventory associates the branch with a task whose landing is unsettled, and Git reports a clean checkout. | Keep it. Land or abandon the task first. |
| `uncommitted` | Git reports tracked or untracked content different from `HEAD`. | Keep it. Preserve the content as a verifiable patch or branch before any later cleanup. |
| `active_task` | The broker inventory associates the branch with a non-terminal task. | Keep it, whether or not it currently looks clean. |
| `unknown` | Ownership, status, or broker evidence is absent or unreadable; the checkout is not an exact path-and-branch match; or a nested repository/content filter makes Git status incomplete. | Keep it. Restore the missing evidence or have its owner dispose of it. |

The exact broker task record is ownership evidence. A location under a worktree root is not. This
is why a checkout made by another session, including one under a familiar temporary directory,
stays `unknown` unless the broker actually associates it with this task.

## Commands and exit status

The default is a dry run:

```sh
tools/check-worktrees.sh
tools/check-worktrees.sh --apply
```

Audit mode follows the repository's four-answer convention:

| Exit | Meaning |
|---:|---|
| 0 | The evidence was complete and there is no remaining removal candidate. |
| 1 | The dry run found one or more proved removal candidates. Nothing moved. |
| 2 | Git, the repository, or an explicitly supplied evidence file could not be read. |
| 3 | At least one linked checkout is `unknown`, or a final apply guard could no longer decide. Proven candidates may have been removed, but unknown paths were not. |

An unavailable daemon is not reported as an empty inventory. The script still reports locally
visible dirty content, classifies the rest as `unknown`, and exits 3. `--apply` never turns missing
broker evidence into permission. It never runs `git branch -d`, `git branch -D`, or an equivalent;
removing a worktree leaves its branch and commits in the repository.

Run the dry check after each landing is recorded. This timing matters: before the landing record,
the system must say `unlanded`; after it, the broker can prove both the target and the absent child
owner. The daemon's own periodic reclamation remains the backstop for broker-owned task trees. The
repository guard is the visible receipt at the root's integration boundary.

## Root-owned temporary trees

Deployment snapshots and comparison trees should not enter the audit backlog at all. Pair their
creation and disposal in one process:

```sh
tools/check-worktrees.sh --ephemeral -- ./tools/package-macos.sh
tools/check-worktrees.sh --ephemeral --rev HEAD~1 -- sh -c 'some read-only comparison'
```

The wrapper creates a detached worktree below the operating system's temporary directory, runs the
command from that checkout, and removes it on exit only when it is clean and its `HEAD` is contained
in `main`. It does not use `--force`. If the command writes uncommitted content, moves to an
unlanded commit, or Git refuses removal, the wrapper prints the retained path. The command's exit
status is preserved. This creation-time pairing is preferred to asking a later sweep to guess who
owned an anonymous scratch checkout.

## Recovery

For `uncommitted`, first snapshot the complete non-ignored tree and prove that the saved patch or
preservation branch reconstructs it; the daemon reclaimer's implementation in
`internal/app/orchestrator/reclaim.go` is the reference. For `unlanded`, record a verified landing,
an explicit abandonment, or `nothing_to_land` as appropriate. For `active_task`, wait for the task
to reach a terminal state and for its owner to leave. For `unknown`, repair the broker or Git
evidence; do not move the directory to make the warning disappear.
