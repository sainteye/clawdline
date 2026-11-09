# Inventory spends less time starting Git processes

This follow-up targets the full inventory wait before foreground coordination.
Baseline source is `8c0451b2fe7d6b1edd968e8f4eb4f6c7e55e5187`. It does not
change the wire response, authentication, database schema, dispatch defaults or
independent destructive reclamation checks. Installation is separate acceptance.

## Measure the binding stage first

The same private fixture used for the earlier review contains 5,740 tasks,
5,053 reclamation rows and 5,506 notices. On the current host it selects 540
historical worktree records, makes 120 branch-presence calls, 42 ancestry calls
and 39 commit-count calls. The warmed ledger takes about 9 ms and the 540
individual reclamation queries about 24 ms. Git branch and ancestry reads take
about 572 ms and 666 ms respectively. These are stage medians, not additive
percentiles of the complete operation. The SQLite batch-read hypothesis was
therefore deferred; it does not explain this request's seconds of waiting.

## Changes retained

- Ancestry uses one `merge-base --is-ancestor` process. Exit 0 means merged,
  exit 1 means known-unmerged, and missing/invalid refs, cancellation and other
  failures remain unknown. `IsAncestor`, which also exposes explanatory errors
  to other callers, is unchanged.
- A lazy branch-presence read batches the selected historical records by their
  Git repository. Results live only within this projection and contain only
  requested names. Literal namespace matching is preserved; wildcard and
  trailing-slash legacy patterns retain individual Git interpretation. A failed
  batch, including an operating-system argument limit, falls back to individual
  reads instead of answering known-absent. No new numeric limit or persistent
  cache is introduced; the request's existing record list bounds the projection.
- Existing checkouts retain their immediate individual branch read. A concurrency
  reproducer creates an unmerged branch after a batch first observed absence;
  an existing checkout must not become an orphan disposal candidate. That test
  failed against the initial batch candidate and passes with this restriction.
  Missing checkouts may use the per-request branch reading; ancestry, commit
  counts, owner checks and later destructive checks still use their own evidence.

Cost: one temporary projection of the selected records and requested branch
answers, plus a fallback path for legacy/error cases. Reading fails safely but
may cost one extra unsuccessful process before the original individual reads.
No queue, concurrency increase, long-lived TTL, or reclamation policy change is
part of the improvement.

## Complete-operation observations

Environment: Apple M4 Pro, 24 GiB RAM, darwin/arm64, Go 1.27.1. The host's other
work was shared; no production load generator or paid content API was used.

| Observation | Before median / P95 | After median / P95 | Samples per arm |
| --- | --- | --- | --- |
| Private core inventory replay | 1573.05 / 1755.31 ms | 660.30 / 768.19 ms | 21 |
| Synthetic complete inventory HTTP | 490.12 / 575.93 ms | 13.07 / 15.98 ms | 5 |

The core replay alternates before/after, after/before, before/after, with seven
observations per invocation. Both binaries have the same task-local stage
instrumentation; those probes are removed from production source. The copied
SQLite store and fixed owner-source snapshot are isolated. Host Git refs,
filesystem and process readings are shared; branch refs were verified unchanged
through the comparison. Every returned payload and generation matched after
normalizing only age, observation time and the isolated task root. No failed or
unknown row was discarded to obtain the result. The core median reduction is
912.74 ms; the phone's critical-path saving has not been measured.

The existing `BenchmarkBrokerHistoryHTTP/inventory` uses 128 synthetic retired
tasks with recorded clean removals and missing delivery branches, plus one live
member. Five operations per arm alternate order using fixed compiled binaries
and one excluded warm-up per arm. It includes real loopback HTTP, the inventory
handler, SQLite, Git and serialization; fixture membership and HTTP 200 are
asserted. Authentication middleware, terminal discovery, the installed daemon,
Cloud transport and phone rendering are outside this benchmark. Its smaller
fixture does not predict the private replay's absolute time. P95 is nearest-rank;
with five samples it is the maximum, not a long-term tail guarantee.

## Correctness and repeatability

Resource regressions were first demonstrated against the old source: one
ancestry question started three processes instead of one, and 32 missing delivery
branches started 33 processes instead of two (repository resolution included).
Tests retain known-unmerged versus unknown, branch-prefix and legacy patterns,
repository isolation, batch failure, changed branch state on the next request,
existing-checkout concurrency, dirty/unknown cleanup evidence and inflight scope.
The initial concurrency near-miss is retained as a regression, not rationalized
as acceptable staleness. Existing checkouts still ask immediately.

Run the public checks from a task-owned worktree and cache:

```sh
GOCACHE="$EVIDENCE/go-cache" tools/check.sh --quick \
  ./internal/adapters/git ./internal/app/orchestrator \
  --run='Test(Merged|BranchPresence|Inventory|ArchivedInventory|Inflight)'
GOCACHE="$EVIDENCE/go-cache" go test -race \
  ./internal/adapters/git ./internal/app/orchestrator \
  -run '^Test(Merged|BranchPresence|Inventory|ArchivedInventory|Inflight)' -count=1
GOCACHE="$EVIDENCE/go-cache" go test ./internal/transport/http \
  -run '^$' -bench '^BenchmarkBrokerHistoryHTTP$/^inventory$' \
  -benchtime=1x -count=5
GOCACHE="$EVIDENCE/go-cache" tools/check.sh
```

The exact private replay helpers, immutable fixture, compiled-binary digests,
per-sample logs, Git-ref fence and comparison runner remain in the task's private
evidence bundle. Replaying it requires that owner-local bundle; the public HTTP
benchmark needs only synthetic data. The retained report records the executed
commit gate and race outcomes separately from these rerun commands.

The task reused the already prepared Performance Optimization instruction skill
at `addyosmani/agent-skills` commit
`8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`, entry
`skills/performance-optimization/SKILL.md`, verified SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`.
Its MIT license, relative references and task-local preparation are retained in
the earlier report. No measurement dependency or global skill was installed.
The role's read/applied receipts are recorded for this task.

## R1 correction

The read-only review found that a missing checkout's unresolved work could be
hidden when its branch returned after an earlier batch. The corrected predicate,
red/green regression and fresh three-version measurements are recorded in
[the R1 correction](inventory-r1-correction-2026-11-09.md). These later checks
supersede any interpretation that the original candidate was ready to install.
