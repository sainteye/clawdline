# Unresolved work stays visible when its branch returns

This corrects R1 from the read-only review of
`1e1a242e27ee22f5c09fb30c4361b73a91203cdc`. The original application baseline
is `8c0451b2fe7d6b1edd968e8f4eb4f6c7e55e5187`. No push, deployment, dev
installation or additional reviewer is part of this correction.

## Failure and the smallest correction

A terminal legacy task can have no Landing record and no checkout directory.
If another writer restores its unmerged branch after the request's earlier
branch batch, the previous candidate classified that task as settled and hid it.
An immediate read instead reported unlanded work. Pending and unknown Landing
states could also receive stale branch evidence.

Unresolved Landing states now always use the immediate individual BranchExists
read, whether or not the checkout still exists. Existing checkouts retain that
same immediate check. Only already-settled records whose checkout is absent can
use the request's batch result. This reuses the existing settledLanding
classifier; it adds no state, cache lifetime, wire field, permission, queue or
reclamation rule. The ancestry optimization and independent destructive checks
are unchanged. Cost: unresolved rows that reach this stage retain their original
individual Git read.

## Regression evidence

The new tests first failed on the old candidate and pass after the correction:

- Legacy/no-Landing, pending and unknown future states are compared with the
  individual row reader after a branch returns inside the same request.
- A settled record shares the same branch name, so merely filtering batch inputs
  would not prevent the unresolved record from reusing stale evidence. That
  input-filter-only near-miss is tested through a task-local Go overlay and fails.
- A complete ReadInventory injects branch restoration after its first batch read,
  before a later legacy row is classified. The restored task remains in Unlanded
  with land_or_abandon, its response and generation equal a subsequent fresh
  inventory, and the inflight consumer still lists it.
- Existing-checkout concurrency and settled-history Git process budgets remain
  covered by the existing tests.

These are deterministic event-order reproductions using isolated Git repositories
and a callback; they do not require a flaky sleep or alter the live store. They
cover the broker projection and inflight consumer. A full HTTP Session-close
interleaving, Cloud/phone interaction and an installed App are not verified here.

## Remeasure all three versions under the same conditions

Measured on macOS-15.7.3-arm64-arm-64bit-Mach-O, Apple M4 Pro,
go version go1.27.1 darwin/arm64. GIT_OPTIONAL_LOCKS=0; a task-owned Go cache was used.
No other task-owned compile or check ran during the serial timing interval.

The private fixture remains 5,740 tasks, 5,053 reclamation rows and 5,506 notices.
Each invocation imports an isolated SQLite store and warms its decoded ledger.
No task-local production instrumentation is inserted: one identical private test
helper is supplied through Go overlays at compile time. Three source binaries
run serially, with rotating order over three cycles and seven reads per invocation.
Compilation and correctness checks finish before measurements start. Other host
work remains shared. Git branch refs are fenced before and after the comparison.

| Core inventory replay, 21 observations per version | Median | P95 |
| --- | --- | --- |
| Original 8c0451b | 1599.605 ms | 1780.417 ms |
| Prior candidate 1e1a242e | 703.972 ms | 786.854 ms |
| R1 correction | 724.612 ms | 831.471 ms |

Ordinary frozen-fixture payloads and generations match across all 63 observations,
normalizing only age, observation time and the isolated task root. The R1-specific
regression separately proves the intended correction to the failing interleaving;
normal frozen-fixture equality alone cannot prove that correction.

| Synthetic complete inventory HTTP, 5 observations per version | Median | P95 |
| --- | --- | --- |
| Original 8c0451b | 501.476 ms | 665.106 ms |
| Prior candidate 1e1a242e | 15.090 ms | 38.401 ms |
| R1 correction | 13.131 ms | 54.935 ms |

The HTTP measurement reuses BenchmarkBrokerHistoryHTTP/inventory with 128 settled
synthetic tasks plus one live member. Every version has one excluded warm-up,
then five serial operations in rotating order. HTTP 200 and fixture membership
are asserted. This includes the real handler, SQLite, Git and serialization;
authentication, terminal discovery, the live daemon, Cloud and phone rendering
are outside the benchmark. This all-settled fixture does not measure the added
individual reads for unresolved tasks; the private core fixture includes them.
Nearest-rank P95 is reported; with five samples it is the maximum, not a long-term
production tail estimate. Different fixtures' absolute times are not comparable.

The corrected core median saves 874.993 ms per replay versus the original application baseline. Restoring immediate evidence for unresolved work costs 20.640 ms relative to the incorrect prior candidate in this run. These are absolute waits on this fixture, not a phone latency claim or a new speed gain from the correctness patch. Shared-host timing and the small HTTP sample limit the tail estimate; branch freshness takes precedence over the prior candidate's lower timing.

The earlier report's figures are historical evidence, not reused as the current
comparison. This round's commands, binary SHA-256 digests, source snapshots,
source-ref fence, load snapshots and full samples remain in the private task
bundle. There is no new timing threshold in CI and no added measurement package.

## Verification and remaining scope

```sh
GOCACHE="$EVIDENCE/go-cache" tools/check.sh --quick \
  ./internal/app/orchestrator --run='TestInventory(MissingCheckout|ExistingCheckout)'
GOCACHE="$EVIDENCE/go-cache" go test -race ./internal/app/orchestrator \
  -run '^Test(Inventory|ArchivedInventory|Inflight)' -count=1
GOCACHE="$EVIDENCE/go-cache" go test ./internal/transport/http \
  -run '^$' -bench '^BenchmarkBrokerHistoryHTTP$/^inventory$' -benchtime=1x -count=5
GOCACHE="$EVIDENCE/go-cache" tools/check.sh
```

The exact private replay needs the owner-local fixture and its retained helper;
the synthetic HTTP benchmark is runnable without private data. The correction's
receipt records checks actually executed separately from these rerun commands.
Full CI/cross-platform caller runtime checks, deployment and App/phone acceptance
remain separate. No additional review task or usage-priced external API was used.

This task reuses the already prepared Performance Optimization instruction skill
at addyosmani/agent-skills commit
`8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`, entry
`skills/performance-optimization/SKILL.md`, SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`.
The full task-local source, relative references and MIT license are retained;
no dependency installation or native skill loading is claimed.
