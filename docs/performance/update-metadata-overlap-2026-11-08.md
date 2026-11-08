# Fresh update checks download their verification inputs together

This change removes one serial network wait from a complete foreground update
check. The manifest and its signature list are downloaded together; neither is
accepted until the existing `release.Open` verifies the signature and validates the
manifest. It changes no update command, installation, release key, result envelope,
freshness rule or automatic-retry policy.

The selection follows [the user-impact priority rule](local-api-review-2026-11-08.md).
A fresh check controls when the update panel can offer a current release and is
also used before an installation. It does not block the Session list itself.
The previous review observed 49 foreground checks in its one-hour window, but that
is not a measured current hourly rate or a claim that their summed time was all
human waiting. The running daemon had since changed to stable `v2.15.1` and was
successfully checking for updates, so the earlier timeout observations were not
reused as this experiment's baseline. Native launch failures remain a separate
investigation; no terminal was opened to reproduce them in this round.

## Baseline, controls and measured effect

Source baseline: `810d02de1fbb53ee49f5c44545c3e54f018b9941`, on top of application
source `4b106f223f1a632c7817d8140085b0a9a079d60f`. The experiment used an isolated
checkout on Apple M4 Pro, 24 GiB RAM, Go 1.27.1, darwin/arm64. Shared background
load was not controlled. The network experiment and controlled HTTP benchmark
answer different questions; they are not interchangeable service-level guarantees.

| Measurement | Before median / P95 | After median / P95 | Absolute median saving |
| --- | --- | --- | --- |
| Complete local `GET /v1/update/fresh`, 800 ms delay per metadata response | 1605.31 / 1606.59 ms | 803.17 / 803.96 ms | 802.14 ms |
| Verified current release metadata from the real update source | 477.58 / 523.65 ms | 160.94 / 257.06 ms | 316.64 ms |

The controlled benchmark served signed fixtures through the real HTTP update
handler and checker, including both downloads, Ed25519 verification, manifest
validation and JSON status serialization. It used five benchmark runs with exactly
one complete foreground HTTP operation each, no warm-up exclusion, and the same
800 ms delay before each metadata download in both arms. Every response was HTTP
200 with `update_available` for the signed fixture release. These are handler-path
measurements without the outer authentication gate, phone network or relay.
The latency shape was motivated by two initial direct metadata downloads of
922.96 and 789.65 ms; it is not presented as an exact recording of the network.

The real-source experiment compared the original sequential download/verification
path with the candidate using one HTTP client/connection pool and the same trusted
release keys. Both paths were warmed once, then each was run five times, alternating
which arm went first on each cycle. Every measured operation succeeded and verified
`v2.15.1`; no faster failure was counted as a success. The original samples were
352.31, 477.58, 514.64, 523.65 and 458.43 ms; the parallel samples were 142.36,
160.94, 203.73, 257.06 and 146.82 ms. Even the slowest parallel sample was below
the fastest sequential sample in this bounded experiment. P95 here is nearest-rank
with five samples, hence the maximum, not an estimate of long-term tail latency.

There were 24 logical metadata reads including four warm-up reads, and 72 HTTP
transport requests including redirects, against a hard cap of 72. A ten-second
operation context matched the foreground handler's existing deadline. There were
no added application retries. These were ordinary public release-asset downloads,
not usage-priced API calls. Two earlier diagnostic curl downloads are separate
from that experiment's request count. No updater apply/install operation occurred.

The results support retaining this change: it removes a complete serial dependency
on an action that really waits for a fresh verified result, with an absolute saving
of about 0.3 seconds on the warmed source and 0.8 seconds in the controlled slower
network case. They do not establish an improvement to the currently running
installation or to a physical phone until that destination is updated and measured.

## Implementation and costs

`updater.Env.Fetch` starts one signature download beside the manifest download,
then joins both before verifying. A manifest read failure keeps its original error
precedence, cancels the sibling and joins it before returning. A signature 404
still means `manifest_unsigned` when the manifest was read successfully. Missing
manifests, invalid signatures and cancelled reads remain typed failures. No content
is trusted because it arrived first, and no previous signature replaces a current
read.

The fixed pair is registered as `release.metadata_concurrency`: two reads per
verification, no third request or retry queue. A capacity override can lower it to
one, preserving the original sequential verification; raising it is refused. Existing 64 KiB manifest, 16 KiB
signature and per-file timeout bounds remain. The HTTP foreground deadline remains
unchanged. The row has transport measurement wiring, both capacity snapshots,
generated agent references, and English/Taiwan Traditional Chinese explanations.
The measurement describes a per-verification guard, not a machine-global occupancy
counter or an observed zero queue.

The cost is one additional short-lived goroutine per verification and simultaneous
use of two network reads. A failed manifest can now have already started one
signature request that the sequential path would never send; it is cancelled and
joined. A slow or failed origin can still consume the existing deadline. This does
not solve upstream availability or reduce the timeout by returning a premature
refusal. Successful checks still require exactly two metadata downloads.

## Verification and reproducibility

The overlap test holds the manifest response until the signature request arrives.
It failed on the sequential implementation with a deadline error and passes with
parallel reads; it also checks that the fixed pair is the entire request count.
Failure-injection tests cover manifest 404, manifest error taking precedence over
signature 404, missing/invalid signatures, mismatched signed generations, cancellation of a sibling after a
manifest error, and cancellation of both active downloads. An HTTP-level test warms
a valid cached release, fails the current signature request with 504, and proves
that a fresh response is `unknown` with no latest release offered. Existing tests
retain withdrawn-release and cached-status behavior.

The new concurrency declaration was added before registration:
`TestEveryBoundIsRegistered` failed specifically on
`internal/adapters/release/updater.MetadataReadConcurrencyLimit`; registration then
made it pass. Targeted updater/HTTP tests and the updater concurrency tests under
`-race` passed. The repository commit gate is a separate check, recorded in the
Session delivery rather than implied by the measurements here.

```sh
# Complete foreground HTTP operation, controlled latency; no external traffic.
GOCACHE="$EVIDENCE/go-cache" go test ./internal/transport/http \
  -run '^$' -bench '^BenchmarkFreshUpdateVerification$' \
  -benchtime=1x -count=5 -benchmem
GOCACHE="$EVIDENCE/go-cache" tools/check.sh --quick \
  ./internal/adapters/release/updater ./internal/transport/http \
  --run='TestFetch|TestFreshUpdate|TestTheStatusOfAReleaseInstall|TestAReleaseInstallNeverReadsTheHostedBuild'
GOCACHE="$EVIDENCE/go-cache" go test -race ./internal/adapters/release/updater \
  -run '^TestFetch' -count=1
```

The private task evidence retains before/after logs, the red tests, the network
comparison helper and its request counter. That network helper is not committed or
run by CI; it requires explicit `REAL_UPDATE_MEASURE=1` and uses the recorded
72-request cap. Its original command was:

```sh
# Restore the task-local helper into an isolated checkout before rerunning.
GOCACHE="$EVIDENCE/go-cache" REAL_UPDATE_MEASURE=1 \
  go test ./internal/adapters/release/updater \
  -run '^TestLocalRealUpdateComparison$' -count=1 -v
```

The round reused the task-local instruction skill from `addyosmani/agent-skills`
at immutable commit `8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`. Its entry
`skills/performance-optimization/SKILL.md` still matched SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`; the retained
full clone includes its required references and MIT license. It was manually
adopted, with Clawdline read/applied receipts, without a global plugin installation.
