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
The Session close, archive, and deferred self-close paths now write a distinct
`root_assignment.closed` receipt into the assignment row and event log after
the exact executor closes. The close route's Root identity comes from the
fresh Session projection, which binds the terminal, assistant and process
launch interval; the stored assignment is checked again for its terminal,
backend and assistant. A replay leaves the first receipt unchanged. The
receipt names the conversation, method, timestamp and whether the close was
forced. A forced close records closure but is not completion evidence. A
refused close or a self-close merely scheduled for later writes no receipt.
If closing succeeds but storing this receipt fails, the response says so and
the Root remains unproved for cleanup.

The supported close paths and their records are:

| Path | Close record | Root receipt |
| --- | --- | --- |
| Person's Session close | `app.Actions.ClosePinned` attempts `session.closed` after terminal close; that event writer logs a store failure but does not fail the close | `root_assignment.closed` in the assignment transaction after the verified executor closes; `forced` reflects the request |
| Person's archive | The same close event, then an archive row; `archive_not_recorded` can follow a successful close | The Root receipt is still attempted for that successful close, including when the archive row fails |
| Agent's immediate self or Epic-owner close | The same close event after a safe close | A safe Root receipt only when the fresh Session projection proves the exact Root executor |
| Agent's deferred self-close | `session.close_scheduled` is logged when queued; the queue itself is in memory. The sweep repeats the close audit, calls the same close action and logs `session.closed` after success | A safe Root receipt is written only after the sweep actually closes the Session; a dropped schedule has none |

The scheduled-close event writer also logs store errors rather than claiming
durability. The assignment row and its `root_assignment.closed` event share a
transaction, so that separate receipt is the durable Root close fact.

GET `/v1/orchestrator/root-assignments` and its single-assignment read carry a
fresh read-only cleanup projection. It separately reports safe completion,
owner present/absent/unknown, scratch registration, and preservation. Owner
absence requires a complete reading from the executor's terminal source with
another terminal seen, plus a readable process CWD table with no process
inside the project or brief directory. This is conservative for shared
projects. There is no broker-registered, separately owned Root scratch path,
and the Root may hold unlanded bytes in a worktree or other files; scratch is
`unregistered`, preservation is `unverified`, and eligibility is always false.
The `root-assignments/<id>` directory holds the brief and sometimes a handoff
pack, so this change never deletes that directory, a Root worktree, or a cache.

## Go build caches

Go's default build cache is shared by commands of the same user. Go validates
cache keys and periodically removes entries not used recently. `go clean
-cache` removes all of its build entries and forces recompilation on the next
build. The shared cache is rebuildable, but it is not a task-owned directory:
the task reclaimer must never chmod or remove it. Ordinary task, Root
Assignment, and Agent launch commands do not set `GOCACHE` or `GOMODCACHE`;
they inherit the normal Go environment. The content-addressed Go build cache
supports concurrent builds, so these launches do not need a fresh cache for
correctness. The verification gate is different: its read-only checker has
only three broker-owned writable scratch directories, so its launcher sets
`GOCACHE` to the exact task `work/cache` and `GOMODCACHE` below it. Sharing
the user's normal cache would cross that filesystem permission boundary.

Before a gate checker launches, the broker validates its task directory and
scratch as spelled and resolved, refuses symlink escapes, and registers the
exact `work/cache` path in SQLite's `broker_go_caches` with owner kind `task`,
task ID, purpose, rebuildability, and creation time. The row also retains the
last actual cleanup outcome, reason, and time. A dry run changes none of them.
The cache has no independent deletion schedule: its cleanup result follows
the existing task `work/` decision. The broker records a `removing` intent
before unlinking, keeps an active or unknown owner, and retries a failed
removal on the next sweep. A missing scratch after a recorded intent becomes
`removed`; without that intent its disappearance is `unknown`. Read-only
module directories are made traversable only inside proved task scratch;
symlinks are never followed to their targets. A gate cache created by an older
daemon has no registration row, but its task-owned `work/` still follows the
existing task owner proof and reclaim decision. The broker does not invent a
creation timestamp or registration for that older cache.

Root Assignments and Agent launches do not create a separate Go cache in the
broker's launch code. A session may manually set `GOCACHE` somewhere else,
but that directory has no broker ownership receipt and is not registered or
reclaimed by this mechanism. The register operation is intentionally internal
to the gate dispatch; an arbitrary path, including a shared GOPATH or
GOCACHE, is not admitted as gate scratch.

Before an operator clears the shared cache, read `go env GOCACHE`, the active
compile lease and queue, and whether other Go commands are using it. Wait for
builds to finish. Record the exact cache path, approximate bytes by age,
request count if an external service is involved, and filesystem free space
before and after. Do not infer reclaimable space from a sum of directory sizes
or from allocated bytes alone. An unavailable process or lease reading is an
unknown owner, not proof that the cache is idle.

## Remaining product work

These are implementation gaps, not permissions to delete existing data:

1. If Root or Agent sessions gain a Clawdline-managed temporary cache creation
   path, add an authenticated exact-path registration and an owner completion
   receipt before reclaiming it. Manually created paths remain outside this
   broker's cleanup authority.
2. If Root sessions gain broker-owned rebuildable scratch, register its exact
   path at creation. Only then consider removal after safe close, a fresh
   owner-absent reading, and independent proof that worktree or other unlanded
   bytes are preserved. A forced or uncertain close is not that proof.
3. The machine dashboard now reads filesystem free space and grouped standing
   reclaim decisions through `GET /v1/machine/usage`, including failure counts
   and fixed reason codes. It does not show private paths or task identifiers.
   A per-decision last failure and an operator action for retry remain future
   work; the machine summary must stay a bounded read rather than scan caches
   on each page view.
4. Continue testing changed worktrees and filesystem races with failure
   injection. Gate-cache tests cover interruption and retry, active and
   unknown owners, symlink escapes, and read-only directories with an external
   target that stays untouched.

Any new retention window, byte ceiling, row limit, or scan bound belongs in
`internal/domain/capacity` and its registration guard. A cleanup report should
distinguish `removed`, `kept`, and `unknown`, give the reason for each kept
subject, and state the next owner when a person must resolve it.
