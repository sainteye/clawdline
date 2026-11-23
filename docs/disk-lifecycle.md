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
evidence that the owner left, accepted saved results and an unchanged protected-output
manifest. It rechecks the scratch tree, owner and open readers after recording
intent. All terminal outcomes keep scratch for at least 24 hours. Missing output
proof, nested repositories and uncertain linked trees keep the scratch for review.
Only then does it remove the task's `work/`.
`CHILD.md`, `result.json`, and `artifacts/` remain evidence for the Root. Go
modules downloaded into `work/` may have read-only directories; the removal
prepares directories under that proved scratch path for deletion and leaves
shared module caches and symlink targets alone. A failed removal remains a
`kept` decision with `remove_failed` and can be reconsidered on a later sweep.

### Why task scratch stayed after its task ended (measured 2026-11-23)

The machine's task directory held 17 GB, 12.6 GB of it in 100 task `work/`
directories. The reclaimer was not idle: `broker_reclaim` recorded 1,009 removed
task scratch directories totalling 208,227 MB. What stayed had standing `kept`
decisions:

| Standing reason | Tasks | Size at first measurement | Cause |
| --- | ---: | ---: | --- |
| `outputs_unverified` | 38 | 3.2 GB | The saved-result check compared the on-disk `result.json` with the accepted result by `reflect.DeepEqual`. A child writes `"symbols": []`; the store keeps the result with `omitempty`, so it decodes as `nil`. 37 of the 38 differed only that way. The check now compares both as the store encodes them (`sameSavedResult`). |
| `unreadable` (`tree: unverified`) | 13 | 8.5 GB | The scratch tree holds symbolic links (a `node_modules` copy) or a nested `.git` (a repository copy such as `run1/`). Both are deliberate review boundaries (`TestLegacyUnregisteredGateScratchKeepsLinksForReview`); they were not relaxed. |
| no decision yet, `within_grace`, `owner_present`, `owner_unknown` | 50 | 0.96 GB | Not yet eligible, or not yet visited by the advancing 64-subject sweep. |

Other cleanup removed most of these directories during the measurement, so
the counts are a snapshot, not a standing total. Children should keep
repository copies out of `work/` when a worktree or `--ephemeral` checkout
would do, because a nested repository keeps the whole scratch directory for a
person to review.

Two `/tmp` sources were separate. The HTTP package's `TestMain` left one
`clawdline-http-test-home-*` per killed or timed-out run (17,873 found); homes
now live under `$TMPDIR/clawdline-http-test-homes/<pid>-<random>`, and the next
run removes those whose PID the kernel reports absent. Root Sessions' copies
under `/tmp` (24.6 GB of `clawdline-*`, 27 of them registered worktrees) had no
owner record; `tools/check-worktrees.sh --root-checkout` now creates them with
one ([worktrees.md](worktrees.md)). Neither change adopts or removes what was
already there.

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

On Linux, `tools/heavy.sh` uses a persistent wrapper-owned Go cache across
worktrees instead of clearing all Go caches after every command. The root
has a private ownership marker and lock; symlinked or unmarked roots are
refused. Explicit `GOCACHE`/`GOMODCACHE` values stay with their owner, including
broker gate caches. The user's default shared caches are never reclaimed by
this wrapper. Its cache is not a Session-owned disposable directory.

The registered managed-cache budget is 4 GiB, with a 2 GiB filesystem reserve.
Before and after the command, the wrapper measures disk and cache usage. Only
budget or disk pressure triggers reclamation, oldest recognized compiled entries first and
modules only if needed. A still-low preflight refuses the build. Command
failure survives cleanup failure; unreadable ownership or measurements never
authorize deletion. This is a between-command budget, not an in-flight quota.
The implementation, tests and diagnostics are described in [checks](checks.md).
The Linux user-service deploy uses the same wrapper. The previous policy
(2026-10-14) purged build, module, test and fuzz caches after every command;
it avoided indefinite growth but forced each next check to start cold.

### The default shared Go cache outside Linux (2026-11-23)

On 2026-11-23 this machine's default cache (`go env GOCACHE`) reached 75 GB,
57 GB of it written within one day, and the disk fell to about 160 MB free.
Go removes only entries unused for five days and has no size ceiling.
`tools/heavy.sh` on non-Linux machines now runs the command through
`tools/run-heavy-shared-cache.sh`, which afterwards, still inside the compile
slot, runs `clawdline go-cache trim` when the installed binary has it. The
trim uses the registered `build.shared_go_cache_*` bounds: a 20 GiB budget and
a 30 GiB free-space reserve trigger it; it removes only names Go writes
(`xx/<64 hex>-a|-d`) in a directory carrying Go's README marker, oldest-used
first; it keeps every entry used within two hours (Go refreshes mtimes at most
hourly, so a running build's entries are newer) and re-reads each entry just
before unlinking. An explicit `GOCACHE` is never trimmed. A failed listing,
more than the scan bound, an unknown free-space reading or cancellation are
typed errors and remove nothing; the command's own exit status is preserved.
Direct `go test` runs outside `tools/heavy.sh` do not trigger a trim but are
protected by the same window.

A separate capacity row, `resources.disk_low_free_bytes`, alerts below 20 GiB
free on the daemon's filesystem through the capacity notice channel: at most
once per notice cooldown (24 hours), recovering quietly. The notice measures
the default Go cache, the daemon's `tasks/` and `/tmp`, each bounded by
`resources.disk_source_scan_files`; an unreadable source is reported as
unmeasured or as a lower bound, never as zero. The alert removes nothing.

Before an operator manually clears the shared cache, read `go env GOCACHE`,
the active compile lease and queue, and whether other Go commands are using
it. Wait for builds to finish. Record the exact cache path, approximate bytes
by age, request count if an external service is involved, and filesystem free
space before and after. Do not infer reclaimable space from a sum of directory
sizes or from allocated bytes alone. An unavailable process or lease reading
is an unknown owner, not proof that the cache is idle.

The [task resource lifecycle plan](task-resource-lifecycle-plan.md) describes the
active managed-resource service. Heavy callbacks share a daemon-created Go cache
per canonical Project, independently of their private non-Go cache and scratch.
Every consumer is durably linked before launch under the plan/apply mutex. The
source adapter requires all consumers to finish, save their outputs and release
their process groups and locks. Unknown readers keep the cache. The Root need not
close. Shared caches stay registered while recent Go entries are reused; guarded
plan/apply trims recognized entries oldest-used first when capacity or reserve
requires it. The 4 GiB per-cache value is an eviction target, not a launch
refusal threshold. Active consumers or unsealed outputs postpone trimming
without blocking work that fits the aggregate budgets and disk reserve. The
periodic sweeper attempts source-backed sealing before reconsidering eviction;
it does not require a person to publish completed callback outputs. A compile
request that fits aggregate budgets and the reserve returns after measurement,
without waiting for optional cache inspection, output hashing or eviction. Unknown
filenames remain protected. Only unresolved aggregate cache/scratch pressure,
low filesystem reserve or unknown measurements refuse later heavy work. Cache entries have no 24-hour delay; ordinary
scratch still keeps 24 hours and raw logs seven days. A future Project-scoped Bot
uses the same service, without publication, unpinning or arbitrary-path authority.
Manual legacy/default caches are never silently adopted.

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

The shared managed-resource capacity reading includes existing task `work/`,
registered gate caches and non-artifact callback logs. A cache nested in task
scratch is subtracted from that scratch total and counted once as cache. This
accounting does not adopt an external cache or authorize a new deletion path.
The legacy reclaimer retains its own procedure; pressure admission visits its
advancing pages before refusing new heavy work. Protected artifacts and entire
Root assignment directories remain outside eviction budgets.


## Admission and callback receipts

A heavy callback returns its durable acceptance before resource preparation runs.
Acceptance does not mean the command started. The persisted start effect owns
preparation and the existing callback limit bounds accepted work; replaying the
same task ID never creates another launch. Caller disconnection does not cancel
the accepted job. Cancellation during preparation still fences the eventual
process launch. Preparation failures are recorded with a fresh receipt context,
including when the measurement attempt exhausted its deadline.

Measurements keep their underlying error for diagnostics. Cancellation and a
measurement deadline produce `resource_measurement_interrupted`, rather than
claiming that legacy data is corrupt. Unknown measurements publish no known total
and do not trigger cleanup. Only measured budget or reserve pressure permits a
cleanup attempt on the compile path. A service admission under hard capacity
pressure visits one advancing resource page using the existing page limit. The
broker may then inspect one publication page and one
legacy reclaim page before its final admission recheck; background sweeps own the
remaining work. Unknown owners, changed outputs and live readers remain protected.

A reachable daemon's explicit compile refusal stops the CLI command. Recovery
when the daemon cannot be reached remains a separately reported path; a refused
lease is not reported as held. Regression coverage includes slow admission,
request cancellation, replay, cancellation before launch, expired failure receipts,
unknown measurement retention and pressure-page progress.

## Admission failure correlation (2026-11-18)

A missing resource is only a store lookup that positively returns no row.
Cancellation, deadline expiry and unreadable records retain different typed
causes. Machine refusal diagnostics include the source caller, lookup or
measurement phase, a known opaque resource ID and the server deadline when
one exists. The structured diagnostic also has scalar aliases for the existing
CLI refusal renderer. They omit database error text, filesystem paths and data
contents.
The broker records its admission refusal on a fresh record context; this
proves the server decision, not response delivery to a disconnected client.
Callbacks retain the same safe fields in their durable verdict before launch.
Historical refusals without a resource ID or cause remain unknown; a two-minute
runtime alone does not establish which resource failed or whether it was deleted.

The diagnostic does not alter storage limits, the advancing-page admission
policy, output sealing or cleanup authority. A failed lookup never grants
permission to allocate over it, delete a directory, or rerun a command whose
outcome is unknown.

## Short compile requests and managed preparation

Production compile admission persists a normal bounded lease waiter and returns
`queued/storage_preparing` while one coalesced broker worker prepares storage.
The existing reclamation deadline bounds that work, independently of the short
lease HTTP context. A durable result binds the waiter identity, source policy,
service incarnation and resource revision; the registered 60-second readiness
window bounds reuse. Cancellation is terminal for that request identity. A ready
result neither holds the compile slot nor starts a command. A grant consumes its
proof, rechecks the filesystem reserve and invalidates other readiness. A restart
or managed allocation/cache-use change requires another preparation. Existing holders renew without
repeating storage work; explicit refusals and unconfirmed requests never start
the requested native command.

Read-only usage accounting verifies the managed root and counts entries by their
own metadata without following links. It does not apply deletion-only protected
name rules or issue a deletion grant. Inspect/plan/apply still require complete
ownership, publication, identity and absent-use evidence, including the final
pre-unlink check. Unknown accounting still refuses admission.

Trusted Root check tooling can attach a running callback to its canonical Project
Go cache with `resources use-go`. This uses the existing shared consumer fence;
scheduling intent no longer forces ordinary checks to retain a private compiler
cache. Explicit overrides and isolated verification remain private. Pure wait
callbacks do not attach, and no arbitrary cache path can be adopted.

Settled sources notify a separate coalesced managed-resource worker. It advances
through existing pages, yielding admission serialization between pages, and uses
the same publication and plan/apply service as an authorized Bot. A daemon start
also schedules a catch-up pass. Legacy worktree scans retain their own schedule
and cannot delay this notification. Completion alone is not idle proof, and all
retention, unknown-owner, active-consumer and retained-output rules remain intact.

Admission waits can be cancelled without waiting for another inspection to
finish; their diagnostics name `admission.wait` separately from filesystem
measurement. Shared-cache attachment reconciles an interrupted trim's original
receipt and owned filesystem identity under the existing mutation fence. It
preserves the bytes, does not repeat the trim, and does not take over another
live process's pending execution. An unverified identity stays blocked. Bot and
daemon adapters share this same Service and Store handle rather than creating
parallel executors with separate mutation fences.
