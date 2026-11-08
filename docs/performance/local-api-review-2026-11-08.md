# Choosing local API performance work by user impact

This review is for agents choosing the next daemon API optimization. It records a
local baseline, rejected experiments, and the evidence missing from today's monitor.
It retains no application optimization. The permanent selection rule is also in
`AGENTS.md`, so a future agent reads it before choosing a target.

## Selection rule

1. Address current failures, blocked interactions and disruptive waits first.
2. Among actionable candidates, use observed frequency and absolute time removable
   from the person's critical path. Establish which wait actually delays their action.
3. Treat resource efficiency as a separate benefit, justified by measured current
   contention, cost or capacity pressure.
4. Defer already imperceptibly fast operations. A large relative improvement in a
   small internal step does not establish a worthwhile product improvement.

Record the affected action, observation window, frequency, absolute end-to-end
latency, removable part of that latency, expected saving, correctness risk and
maintenance cost before selecting an implementation. Request count times duration
is aggregate service time: overlapping calls and background checks prevent treating
it as time a person can save. Repeat measurements under equivalent conditions;
keep unchanged behavior and freshness requirements. Revert an experiment whose
benefit remains inside noise or is only established in a microbenchmark.

## Baseline and conditions

The read-only production observations came from the selected local daemon at
`v2.15.0-dev.c664179`. Source analysis and isolated experiments used
`4b106f223f1a632c7817d8140085b0a9a079d60f`, which contains the earlier inventory fix.
The machine was an Apple M4 Pro, 24 GiB RAM, Go 1.27.1, darwin/arm64.
The second one-second host sample showed 51.24% CPU idle, about 9 GiB compressed
memory and 21 GiB swap in use. Background load was shared and uncontrolled.
These observations are a baseline under current conditions, not a clean-host SLO.

Three owner-local history reads used `GET /v1/api-performance?hours=1|24|168`.
The one-hour window was 2026-11-08 17:45–18:45 Asia/Taipei: all 60 complete minute
buckets were covered, with zero dropped observations and zero queued events at
collection. The 24-hour and seven-day windows contained partial coverage; their
counts must not be extrapolated into continuous service or compared as equal builds.
The collector was current, with healthy persistence and three open streams.

| Route | Completed in the one-hour window | Mean | Observed maximum |
| --- | ---: | ---: | ---: |
| `GET /v1/update/fresh` | 49 | 4579.51 ms | 10009.61 ms |
| `POST /v1/orchestrator/tasks` | 7 | 18308.28 ms | 23567.96 ms |
| `GET /v1/orchestrator/inventory` | 7 | 1277.84 ms | 2277.47 ms |
| `POST /v1/voice` | 11 | 1486.11 ms | 1759.76 ms |
| `GET /v1/sessions/:id/info` | 45 | 153.03 ms | 321.62 ms |
| `GET /v1/sessions` | 353 | 3.70 ms | 194.97 ms |

The history's P95/P99 values are histogram upper bounds, not exact latency
samples. With three observations, both can equal the slowest occupied bucket;
a 19.9-second observed maximum legitimately falls in the 30-second bucket.
Check count, maximum and mean before interpreting a percentile. See
[the producer contract](../local-api-performance.md).

Five cycles of low-frequency, serial authenticated loopback reads, one request per
route per cycle, yielded these client completion measurements. There was no warm-up
exclusion; nearest-rank P95 with five samples is the maximum.

| Action | Median | P95 | Samples |
| --- | ---: | ---: | --- |
| Health | 0.44 ms | 0.49 ms | 5 |
| Session list | 1.49 ms | 1.63 ms | 5 |
| Cached update status | 0.54 ms | 0.60 ms | 5 |
| One-hour performance read | 0.72 ms | 0.92 ms | 5 |
| Repository inventory | 3028.38 ms | 3142.09 ms | 5 |

These are normal bounded reads, not a throughput/load test or phone/relay timings.
No dispatch, transcription, terminal opening, installation or restart was triggered
for measurement. The voice and send entries above are existing observations only.

## Highest-impact findings

**Foreground updates: external availability, rather than general daemon slowness.**
Three further foreground reads completed in 10013.78, 9364.24 and 211.85 ms.
They returned HTTP 200 with update state `unknown`; the actual result reported
GitHub release manifest/signature timeouts or HTTP 504. During those reads, cached
update status took 0.78–1.17 ms and health took 0.28–0.41 ms. This establishes a
specific failing dependency path and failure isolation, not the upstream cause.
The short third response was another 504, not a successful recovery. Prioritize
availability diagnosis and any demonstrably redundant request work here. Do not
remove signatures, replace a fresh check with stale success, or shorten the timeout
and describe the resulting refusal as faster successful service. Implementation
cost includes preserving signed-release freshness, coalescing and cancellation.

**Dispatch: measure launch outcomes and stages.** The same diagnostic snapshot
contained seven terminal-opening failures totaling 140.041 seconds, with a
20.120-second maximum. The one-hour dispatch history had seven calls and no 5xx.
The current record does not correlate those individual stages with requests, so
matching counts/timing is a lead, not proof of one cause. A 200 dispatch envelope
also does not establish that the assistant started or accepted its brief.
Investigate this current failure before faster bookkeeping. Do not reopen a
terminal, change integration consent or create a paid agent run to reproduce it
without the applicable authorization. The likely effort is higher than a local
read optimization because native launch, consent, execution and delivery are
separate boundaries.

**Inventory: seconds on a foreground dispatch prerequisite.** It remains worth
profiling, but a smaller helper getting much faster is insufficient. Its earlier
archived-Git optimization is already in the running revision's ancestry. Remaining
Git subprocesses, history reads and process occupancy checks need stage timings
under current task volume, rather than assuming that old fix is absent.

**Already-fast list reads:** do not prioritize their impressive percentage gains.
Revisit them if increased usage, tail latency or measured shared-resource pressure
makes a user effect visible. This review establishes no throughput saturation point.

## Experiment ledger: all changes reverted

The private replay copied only broker tasks, reclamation rows and notices into an
isolated temporary store, substituting a synthetic secret hash. It contained 5740
tasks, 5053 reclamation rows and 5506 notices. It used a fixed session/source
snapshot and existing read-only Git refs with `GIT_OPTIONAL_LOCKS=0`. It did not
open the live business store for writing or run reclamation. There were seven
steady-state inventory reads after warming the decoded ledger. The fixture is
private; raw records, paths, identities and error bodies are not in this repository.

| Experiment | Before | Candidate | Decision |
| --- | --- | --- | --- |
| Early owner precheck before archived Git work | Full replay median 2389.29 ms | Full/process precheck 2637.60 ms; terminal-only precheck 2536.84 ms | Reverted: no endpoint win |
| Remove duplicate spellings from one process snapshot | Synthetic projection median 230.65 ms, P95 243.36 ms; 61.01 MB allocated | 8.14 ms, P95 8.87 ms; 2.25 MB | Reverted: synthetic gain did not establish current user benefit |
| Same deduplication, fixed real directory/candidate inputs | Median 52.08 ms, P95 56.16 ms; 13.17 MB allocated | 19.37 ms, P95 21.01 ms; 4.65 MB | Reverted: about 33 ms in this stage; no demonstrated relief of current resource pressure |
| One authoritative Git ancestry command instead of two preliminary verifications plus ancestry | Helper median 16.78 ms, P95 20.77 ms | 5.28 ms, P95 5.41 ms; command count 3 → 1 | Reverted: helper win alone does not meet priority rule |
| Inventory replay with only the Git candidate | Median 2389.29 ms, P95 2479.99 ms | Median 2232.39 ms, P95 2463.24 ms | Reverted: about 157 ms median, only 17 ms at P95, overlapping ranges and shared-host noise |

Synthetic projections used 600 directory entries, 20 unique paths and 24 candidate
checks: five benchmark runs of three operations. The fixed real-input projection
used 480 entries, 69 unique spellings and 27 candidates with the same repeat count.
Git helper benchmarks used five runs of ten operations on the same two-commit graph
shape. Table P95 values are nearest-rank percentiles of **run averages**, not
request-level tail latency. The full replay's P95 is the maximum of seven samples.
No result here is a demonstrated phone interaction improvement.

All seven normalized public inventory payloads matched before/after: only elapsed
age, observation time and the task-local store root were normalized. Cancellation,
missing refs, absent Git, incomplete owner sources, process errors, changing
symlinks and fresh next-request evidence were checked during experiments. A new
Git command-budget test failed on three commands before the candidate and passed
on one afterward. The existing Git and orchestrator package suites passed with the
candidate. These are experiment checks; they do not certify a retained application
change, production deployment or a release. Temporary tests and candidate patches
remain in the private evidence bundle, not as dead tests in the source tree.

Reproduction commands used the following shapes; the task-local evidence bundle
retains the exact scripts, fixture, logs, candidate patches and skill manifest:

```sh
# Temporary experiment tests must first be restored in an isolated checkout.
GOCACHE="$EVIDENCE/go-cache" go test ./internal/adapters/git \
  -run '^$' -bench '^BenchmarkMergedBranch$' -benchtime=10x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" go test ./internal/app/orchestrator \
  -run '^$' -bench '^BenchmarkInventoryOccupancySnapshot$' -benchtime=3x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" API_REPLAY_EVIDENCE="$EVIDENCE" \
  go test ./internal/app/orchestrator -run '^$' \
  -bench '^BenchmarkLocalPrivateOccupancyReplay$' -benchtime=3x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" GIT_OPTIONAL_LOCKS=0 \
  API_REPLAY_FIXTURE="$EVIDENCE/private-replay.json" \
  API_REPLAY_SESSIONS="$EVIDENCE/sessions-private.json" \
  API_REPLAY_PROJECT="$REPOSITORY" API_REPLAY_OUTPUT="$EVIDENCE/replay.json" \
  go test ./internal/app/orchestrator -run '^TestLocalAPIInventoryReplay$' -count=1 -v
```

## Recording improvements to prioritize next

The existing monitor already has bounded local storage, version/boot provenance,
coverage/loss accounting, cancellation, separate stream lifetime and finite
latency. Preserve those properties. More raw logs are not the goal.

| Gap established by this review | Smallest useful improvement | Cost and acceptance evidence |
| --- | --- | --- |
| 45651 of 174013 seven-day finite observations (26.2%) were unattributed overflow; one-hour overflow was 135 of 1478 (9.1%) | Recover route attribution within explicit capacity/privacy bounds; inspect the lifetime route admission registry, which outlives pruned buckets | Requires cardinality/retention tests and monitor memory, checkpoint/read size and overhead measurements; raising 128 to a larger number alone is not a solution |
| A total request duration cannot separate waiting from useful execution | Fixed, allowlisted stage names for the high-impact paths: admission/queue, store, Git/process inspection, native launch, upstream fetch, response write | Adds instrumentation and bounded aggregates; verify deadlines, errors and stages cannot block business work; measure its own overhead |
| HTTP success can carry an unsuccessful business result | Separate typed outcome/reason counters for update `unknown`, spawn failure, accepted, executed, delivered, observed and acknowledged | Requires contract and failure-injection tests; do not relabel accepted as completed or change existing operation semantics |
| Local completion is not phone receipt or rendering | Correlate a bounded operation across existing owner-channel/client stages; report direct/relay and client completion separately | Transport/client instrumentation and privacy review; no raw identifiers, bodies, commands, credentials or absolute paths in operational aggregates |
| P95/P99 can look exact with only a few requests | Label histogram estimates and always show count, actual window/coverage, observed maximum and low-sample limitations | Reader copy/layout and English/Taiwan Traditional Chinese updates; validate real small-screen states |
| Root causes are hard to reproduce from one combined total | Provide an explicit owner export with build, actual interval, route/stage counts, outcome, coverage/loss and resource context | Separate redaction, byte/retention bounds and export validation; local diagnosis must not silently enable uploads |

These are evaluated follow-ups, not implemented telemetry features or authorizations
to deploy, change permissions, collect conversation content or upload owner data.
Prioritize the first instrumentation slices that distinguish the current update and
launch failures. Do not instrument every path merely because a table is possible.

## Prepared skill and boundaries

The selected instruction source was `addyosmani/agent-skills` at immutable commit
`8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`, entry
`skills/performance-optimization/SKILL.md`, SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`.
A task-local full clone preserved relative paths. The entry, its optimization
patterns, shared performance checklist, README installation instructions, Codex
setup instructions and MIT license were read from that revision and hashed.
Preparation used `git clone --no-checkout`, detached checkout of the fixed commit,
`git rev-parse HEAD`, clean-status verification and `shasum -a 256` of the entry.
This instruction skill has no runtime requirements. It was prepared and manually
adopted in this session, not installed into global agent configuration or claimed
as natively hot-loaded. The source's native plugin route requires a new session;
that broader installation was unnecessary for task-scoped instruction use.

No usage-priced external API was called. Three foreground updater requests made
ordinary existing signed-release metadata reads; no terminal launch, provider
transcription, assistant dispatch, deployment, restart or public release occurred.
