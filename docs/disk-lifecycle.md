# Disk lifecycle and reclaim evidence

Disk cleanup starts with an owner and a recorded disposition, not an age or a
directory name. A task worktree, a task's `work/` scratch directory, a Root
Assignment directory, and the user's shared Go build cache have different
owners and different evidence. Their sizes overlap when one contains another;
do not add them together as a disk total. Compare free space on the same
filesystem before and after a cleanup to measure what it actually released.

## What the broker can currently reclaim

The task reclaimer in `internal/app/orchestrator/reclaim.go` checks a terminal
task record, its exact broker-owned path, the grace period, and positive
evidence that the owner left. Only then does it remove the task's `work/`.
`CHILD.md`, `result.json`, and `artifacts/` remain evidence for the Root. Go
modules downloaded into `work/` may have read-only directories; the removal
prepares directories under that proved scratch path for deletion and leaves
shared module caches and symlink targets alone. A failed removal remains a
`kept` decision with `remove_failed` and can be reconsidered on a later sweep.

The worktree reclaimer refreshes the owner reading, Git registration, checkout
snapshot, landing and target before removal. Uncommitted and untracked
non-ignored content is preserved as a branch and a proved patch. An unknown or
active owner, unreadable Git state, nested repository, content filter, or
unproved landing keeps the checkout. `tools/check-worktrees.sh` is the visible
dry-run guard at the integration boundary; see [worktrees.md](worktrees.md).

The Root Assignment's `briefed` state means that the assignment line reached
its terminal. It does not mean the Root completed, left, or released any file.
The broker therefore must not infer that a Root Assignment directory or a
cache inside it is disposable from `briefed`. A Root-owned cleanup needs a
separate completion receipt, an absent owner reading, a named scratch path,
and preservation rules for files outside that scratch path. Until those exist,
keep Root Assignment data whose purpose or owner is unknown.

## Go build caches

Go's default build cache is shared by commands of the same user. Go validates
cache keys and periodically removes entries not used recently. `go clean
-cache` removes all of its build entries and forces recompilation on the next
build. The shared cache is rebuildable, but it is not a task-owned directory:
the task reclaimer must never chmod or remove it. A separate `GOCACHE` inside
a task's broker-owned `work/` is task scratch and follows that task's reclaim
decision. A cache an Agent creates at another path has no broker ownership
just because its name resembles a Go cache.

Before an operator clears the shared cache, read `go env GOCACHE`, the active
compile lease and queue, and whether other Go commands are using it. Wait for
builds to finish. Record the exact cache path, approximate bytes by age,
request count if an external service is involved, and filesystem free space
before and after. Do not infer reclaimable space from a sum of directory sizes
or from allocated bytes alone. An unavailable process or lease reading is an
unknown owner, not proof that the cache is idle.

## Remaining product work

These are implementation gaps, not permissions to delete existing data:

1. Give task, Root and Agent-created temporary caches a durable registration:
   exact path, owner Session or task, purpose, whether the contents can be
   rebuilt, creation time, and the last cleanup result. A registration must
   name an exact path under an allowed scratch root; a filename pattern or a
   UUID-looking directory is insufficient.
2. Tie Root completion to its Session's actual close or another durable
   completion receipt. Keep `briefed` as a launch state. Reclaim only its
   separately registered scratch after the owner is absent and preservation
   obligations have been resolved.
3. Surface filesystem free space and reclaim backlog with reason counts and
   the last failure. The existing machine-only
   `GET /v1/orchestrator/reclaim` has standing decisions and sweep statistics,
   but the console does not yet present them. Measurement must use cheap
   counters or a cached bounded scan, not walk all caches on each page read.
4. Cover interruption and retry, active and unknown owners, out-of-bound
   symlinks, read-only directories, and changed worktrees with failure
   injection. Existing worktree tests already cover several of these cases;
   any new destructive path needs its own control case that stays untouched.

Any new retention window, byte ceiling, row limit, or scan bound belongs in
`internal/domain/capacity` and its registration guard. A cleanup report should
distinguish `removed`, `kept`, and `unknown`, give the reason for each kept
subject, and state the next owner when a person must resolve it.
