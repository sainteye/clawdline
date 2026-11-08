# Three API waits reduced before a combined review

This round keeps three changes for review: overlap the update verification
inputs, project outstanding work without computing historical cleanup advice,
and count delivery commits only when inventory uses the answer. These target
visible waiting, following [the permanent priority rule](local-api-review-2026-11-08.md).
No running daemon was replaced and no deployment or public release was performed.

## Why these three

The local monitor export covers 2026-11-08 10:52–11:52 UTC, with 3,596 covered
seconds, 59 complete minutes and one partial minute. It spans a restart from
`v2.15.0-dev.c664179` to `v2.15.1`; the response's current build is not the build
of every historical request. These are observed completed requests, including
background callers, rather than counts of distinct people or foreground clicks.

| Action | Observations in that window | Current waiting evidence | Decision |
| --- | --- | --- | --- |
| Fresh update verification | 67 requests | Mean 2,077.75 ms; observed maximum 10,015.98 ms | Remove the serial download dependency; preserve verification |
| Outstanding-work lookup | 1 request | 3,333.43 ms | Low frequency but seconds on a coordination path; avoid evaluating completed work twice |
| Full work inventory | 5 requests | Mean 2,268.38 ms; maximum 3,511.58 ms | Remove commit counts that cannot affect any visible advice or receipt |
| Codex record-path lookup | 1,471 rollout files in a local metadata-only experiment | Seven full-tree searches: median 5.33 ms, maximum 5.98 ms | Rejected: no path cache or identity change for an imperceptible saving |

The record-path experiment deliberately searched the last lexical rollout, and
read directory metadata rather than conversation content. It did not explain the
larger transcript/info request times. The broader recording gaps established by
the earlier review remain relevant; a low-cost inner stage must not be mistaken
for the whole slow request. Request count multiplied by duration is not human
time saved.

## Before and after

Source baseline: `810d02de1fbb53ee49f5c44545c3e54f018b9941`, whose application
source is `4b106f223f1a632c7817d8140085b0a9a079d60f`. Environment: Apple M4 Pro,
24 GiB RAM, Go 1.27.1, darwin/arm64. Background machine load was shared.

| Controlled complete HTTP operation | Before median / P95 | After median / P95 | Median wait removed |
| --- | --- | --- | --- |
| Update check, 800 ms per metadata response | 1,605.31 / 1,606.59 ms | 803.17 / 803.96 ms | 802.14 ms |
| Outstanding work, 128 archived tasks plus one live task | 2,214.37 / 2,529.70 ms | 6.06 / 7.20 ms | 2,208.31 ms |
| Full inventory, the same task shape | 1,048.87 / 1,283.94 ms | 677.04 / 697.51 ms | 371.83 ms |

Each cell summarizes five operations per arm. P95 is nearest-rank, therefore the
maximum with this small sample; it is not an estimated production tail guarantee.
The two history benchmarks alternated before/after order over five cycles using
two binaries compiled once, with each binary's SHA-256 logged on every invocation.
Every HTTP reply was checked for success and fixture membership. Each operation
includes a real loopback HTTP exchange, handler, SQLite reads and serialization;
the work benchmarks also execute real Git. Authentication middleware, real
terminal discovery, Cloud relay, phone transport and rendering are outside these
benchmarks. Update fixtures are signed; neither update nor work fixtures call
an assistant service.

The history fixture uses a canonical, symlink-resolved repository path, 128
settled tasks with clean removal receipts and absent delivery branches, and one
live task. An initial fixture used a different symlink spelling than Git returned,
so no task matched and its timings were invalid. Those results were discarded.
The corrected fixture asserts ledger size and a returned live member; the
original HTTP implementation also fails its Git-command guard, making an empty
fixture unable to establish this improvement.

### Existing historical data, separately replayed

A task-private SQLite copy contains 5,740 tasks, 5,053 reclamation rows and 5,506
notices, with synthetic secret hashes. No live database was modified. The fixed
owner-source snapshot and decoded-ledger warm-up were reused, and optional Git
writes were disabled. Seven core operations per arm gave:

| Core operation on the private replay | Before median / P95 | After median / P95 | Median saving |
| --- | --- | --- | --- |
| Outstanding-work projection | 2,486.61 / 2,635.68 ms | 50.71 / 72.88 ms | 2,435.90 ms |
| Full inventory projection | 2,273.86 / 2,402.79 ms | 2,050.14 / 2,081.84 ms | 223.72 ms |

All seven outstanding-work payloads matched, including the same three unresolved
rows. All seven inventory payloads and their generations matched, including the
same 27 disposal candidates. Only elapsed age, observation time and the isolated
store root were normalized. The core replay excludes HTTP, authentication and
Cloud; its Git references and filesystem readings came from the shared host and
were not an immutable production snapshot. It corroborates the mechanism and
real data volume, rather than proving a phone latency improvement or a universal
saving. The full inventory remains about two seconds on this replay: this change
does not eliminate all of its Git and occupancy costs.

The [update verification report](update-metadata-overlap-2026-11-08.md) separately
records five alternating real-source checks: 477.58 → 160.94 ms median, all
verified as `v2.15.1`. This real-source measurement is not the controlled HTTP
measurement above and is not a timeout or outage fix.

## What changed, and what still must be proved

### 1. Update verification inputs overlap

`updater.Env.Fetch` downloads the manifest and signature list together, joins the
sibling before returning and passes both through the original signature and
manifest checks. Manifest errors retain precedence; a failed signature never
becomes a successful stale result. Cancellation, missing files, malformed or
mismatched signatures and the previous verified cache on a failed fresh check
are covered. The capacity register exposes the fixed per-verification maximum of
two metadata reads; lowering it to one restores serial reads. It is not a global
concurrency cap. Cost: one goroutine and a second request may already have begun
when the manifest fails, in which case it is canceled and joined. No retry or
request scheduling frequency was added.

### 2. Outstanding-work lookup reads its own projection

`Broker.ReadInflight` resolves the same project and preserves the existing typed
refusals, then asks for outstanding rows directly. The repository-wide HTTP
route no longer builds an unused full inventory before doing the lookup again.
The per-task route keeps authentication and repository scope unchanged.

The common row reader applies this projection before Git cleanup checks. Live,
unreadable, unresolved legacy and unknown landing states remain eligible.
Only terminal work that the full row classifier always settles is skipped;
pending landings are not hidden merely because a task ended. A caller's own task
remains excluded. The next request still sees fresh ledger versions and Git
facts; there is no result cache, new TTL, database migration or deletion policy.
Cost: one explicit selection predicate and a shared settled-landing classifier.

### 3. Full inventory counts only where the count matters

`Broker.row` reads the commit count for a merged disposal reason or unresolved
landing advice. It skips this read for settled rows that cannot expose a count
or use it in the generation. Branch existence, target ancestry, dirty/unknown
state and owner occupancy still determine visibility and disposal. Missing refs
remain unknown when unresolved work needs its advice. Destructive reclamation
keeps its independent checks. Cost: the two count-use sites must remain aligned
with advice rules; regression tests retain the empty-branch disposal reason and
unknown-count refusal.

These are local implementations for review. A passing local test cannot prove
that a running stable daemon, a Cloud reader or a published binary contains them.
Installation and real interaction acceptance remain a later authorized step.

## Verification and reusable evidence

The regression guards first failed against the original behavior:

- Update overlap waited for a second request that the sequential implementation
  never started before the deadline; the unregistered new bound was also rejected.
- Outstanding-only projection inspected Git for cleanup it never returned.
- The original repository-wide HTTP route ran 33 Git commands for eight settled
  tasks plus one live member; the new route runs only the project-resolution
  command for that fixture.
- Full inventory counted commits that could not affect its output.

Targeted tests compare outstanding rows with the full projection, preserve
unreadable and future states, check project aliases and invalid project refusals,
retain advice and verify newly written work appears on the next read. The
selected four-package race run passed. Complete gate results and the delivered
revision are recorded with the final delivery, rather than inferred here.

The first complete gate failed: a synthetic egress test assumed request arrival
order and lacked synchronization for parallel handlers, and the semantic-risk
index was not regenerated after new English/Taiwan Traditional Chinese capacity
copy. The test now checks the exact path set with synchronized collection; both
semantic indexes were regenerated. One Antigravity hook test timed out in that
run; the same separately compiled binary subsequently passed three executions.
That is a recorded intermittent failure, not proof that the unrelated timeout's
cause is resolved. No assertion was removed or deadline relaxed.

The permanent HTTP benchmark is `BenchmarkBrokerHistoryHTTP`, with its synthetic
fixture and Git command guards beside it. It is opt-in rather than a fragile
wall-clock CI threshold. The updater has its separate foreground benchmark.
Use the same build, task-local cache and repeat count for before/after:

```sh
GOCACHE="$EVIDENCE/go-cache" go test ./internal/transport/http \
  -run '^$' -bench '^BenchmarkBrokerHistoryHTTP$' -benchtime=1x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" go test ./internal/transport/http \
  -run '^$' -bench '^BenchmarkFreshUpdateVerification$' -benchtime=1x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" go test -race ./internal/app/orchestrator \
  ./internal/transport/http ./internal/adapters/release \
  ./internal/adapters/release/updater \
  -run '^Test(Inflight|Inventory|ArchivedInventory|FreshUpdate|Fetch|SyntheticEgressUpdate|AnUnreadableRow)' -count=1
```

The exact alternating runner, binary hashes, private replay helpers, metadata-only
path measurement, source snapshots and raw logs remain in the task evidence
bundle. Private fixtures are not public repository test data. Reproducing the
historical replay requires that owner-local bundle; the permanent synthetic HTTP
benchmark can be rerun without it. Its helper imports only copied rows into an
isolated store, warms `Broker.Records`, then invokes `Inflight` or `ReadInventory`
seven times with `GIT_OPTIONAL_LOCKS=0`. The environment names are
`API_REPLAY_FIXTURE`, `API_REPLAY_SESSIONS`, `API_REPLAY_PROJECT` and
`API_REPLAY_OUTPUT`; restore the task-local helpers before rerunning them.

### Recording improvements

This round preserves the monitor's existing bounded, versioned history; it does
not claim to have implemented new production stage telemetry. The fixed dataset,
exact commands, invalid-run rejection and repeatable HTTP benchmarks make the
next investigation easier. The earlier review's route-overflow, outcome and
phone-completion gaps remain open. Next instrumentation should use allowlisted,
bounded stage/outcome aggregates for the slow paths, without conversation text,
credentials or absolute paths; its overhead and failure isolation require their
own measurements. More raw request logs alone would not explain these waits.

## Prepared skill and scope

The task reused the full task-local `addyosmani/agent-skills` checkout at commit
`8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`, entry
`skills/performance-optimization/SKILL.md`, verified SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`.
Preparation used `git clone --no-checkout`, checkout of the immutable revision,
`git rev-parse HEAD`, clean-status verification and `shasum -a 256`. The entry,
relative optimization patterns, shared checklist, installation instructions and
MIT license were read from that same revision; all relative paths were retained.
There are no runtime dependencies or package installation requirements for this
instruction skill. It was prepared and manually adopted in this Session, with
Clawdline read/applied receipts, rather than installed globally or natively
hot-loaded. Preparation and the actual task-local path/version are retained in
the skill manifest and prior report.

No usage-priced API was used. The real update-source experiment made 24 logical
metadata GETs, including warm-ups, and 72 transport requests including redirects,
with a hard cap of 72 and no automatic retries. Two preliminary asset downloads
were separate from that experiment. These were ordinary public release reads,
with no usage-priced API charge. No assistant call, terminal launch, deployment,
restart or public release was performed.

## Follow-up inventory measurement

[The inventory Git-read follow-up](inventory-git-reads-2026-11-08.md) profiles the
remaining seconds and verifies request-scoped branch batching and one-process
ancestry reads separately from this round's earlier commit-count projection.
