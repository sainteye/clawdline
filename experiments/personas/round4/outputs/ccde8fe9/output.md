Everything passes. Here's the full report.

## What was slow and why

`GET /report` had two compounding problems in `report.go` (measured with `go test -bench` and a temporary `BenchmarkBuildReport`/`BenchmarkGetUserLoop1000` in-process, no network):

1. **N+1 store calls** — `BuildReport` called `s.GetUser(o.UserID)` once per order (1000 orders), each paying the simulated `queryLatency` round trip (100µs) sequentially. Measured: 1000 sequential `GetUser` calls = **137.5ms**; the same lookups via the already-existing `s.GetUsers(ids)` batch method = **0.146ms** (`report.go:60-66` before fix vs. after).
2. **A package-global `reportMu sync.Mutex` held for the entire `BuildReport` call** — this serialized *every* concurrent request, even though `BuildReport` only reads through `Store`'s own `RWMutex` and builds fresh local values each call (nothing shared is mutated). Result: throughput was flat at ~7.3 req/s no matter the concurrency (1, 8, or 32 clients), with latency growing linearly as requests queued behind the lock.

Regex compilation (`regexp.MustCompile` inside `cleanNote`, called 1000×/request) added ~1.5ms/request — a real but secondary cost.

`uniqueSorted`'s O(n²) dedup was also examined but, given the fixed seed data (only 8 distinct tags, ~300 distinct customers), it measured at 33µs and 190µs respectively — negligible, so left unchanged (see below).

## What I changed (`report.go` only)

- Replaced the per-order `s.GetUser` loop with a single `s.GetUsers(ids)` batch call.
- Removed `reportMu` — nothing it protected was actually shared/mutable across calls; `Store` already synchronizes its own state with an `RWMutex` that allows concurrent readers.
- Hoisted the two `regexp.MustCompile` calls out of `cleanNote` into package-level vars (compiled regexes are safe for concurrent use).
- Left `uniqueSorted`'s algorithm as-is (no measured benefit for this dataset — see "not changed" below).

`store.go`, the HTTP API, and the seed data were not touched.

## Before / after measurements

All runs against `go build -o server .` (not `go run`, to avoid recompilation skewing timing) with `NewStore(300, 1000)`, using the repo's own `./loadtest` tool, prebuilt the same way.

| Command | Before | After |
|---|---|---|
| `loadtest -n 20 -c 1` | wall=2.702s p50=135ms p95=138ms | wall=43ms p50=2ms p95=6ms |
| `loadtest -n 80 -c 8` | wall=10.996s p50=1.078s p95=1.155s | wall=44ms p50=4ms p95=6ms |
| `loadtest -n 160 -c 32` | wall=21.973s p50=4.296s p95=4.442s | wall=45ms p50=8ms p95=17ms |
| `loadtest -n 640 -c 64` | (not run; extrapolates worse) | wall=150ms p50=13ms p95=33ms |

Throughput (`n / wall`): before was flat at **~7.3 req/s regardless of concurrency** (proof the global mutex was the ceiling); after, throughput scales with concurrency: ~465 → ~1818 → ~3556 → ~4267 req/s. Per-request p50 dropped ~135ms → 2–13ms depending on load.

Component-level evidence (`go test -bench`, in-process, isolates the two fixes):
- `GetUser` × 1000 sequential: 137.48ms → `GetUsers` batched once: 0.146ms.
- `cleanNote` × 1000 (regex per call vs. hoisted): 1.54ms → 1.02ms.

## Response unchanged

`curl /report` before and after: identical byte count (124339), identical MD5 (`540b969e5e4391a521fe748f12c38846`), `diff` empty — including after a 200-request/32-concurrency burst (rules out any newly introduced ordering/race artifact).

## Correctness

`go vet ./...` and `go test ./...` pass. Also ran the server under `go build -race` through a concurrent load-test burst (n=200, c=32) — no race reported, confirming removing `reportMu` didn't introduce one.

## What I chose not to change

- `uniqueSorted`'s O(n²) dedup: measured at 33µs/190µs for this seed data, dwarfed by everything else even after the fix — not worth the diff given no measured benefit here (it would only matter if tag/customer cardinality grew much larger).
- `store.go`'s `queryLatency`, seed data, and the HTTP API/response shape, per constraints.

## One housekeeping note

For benchmarking I started two throwaway local servers (`127.0.0.1:8081` and a `-race`-instrumented one on `127.0.0.1:8082`, both localhost-only). I was able to stop the very first baseline server, but subsequent `kill`/`xargs kill` attempts against the two I started later returned `Operation not permitted` — this looks like a sandbox restriction on signaling background processes from a different tool invocation, not something in your control from here. Both are harmless (no repo state, localhost only), but if you want them gone, stop PIDs 96714 (port 8081) and 97203 (port 8082) from your own terminal.
