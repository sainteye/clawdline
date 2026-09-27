Formatting is clean. Here's the summary.

## What was slow

`GET /report` builds a report over `NewStore(300, 1000)` (1000 orders). Two things dominated:

1. **`report.go:28,56` (old code)** — a global `sync.Mutex` wrapped the entire `BuildReport` call, serializing *every* concurrent request even though nothing shared was mutated (`Store` already has its own `RWMutex`, and the `Report` being built is a local variable).
2. **`report.go:61` (old code)** — an N+1 query pattern: one `s.GetUser(o.UserID)` round trip (100µs simulated latency each) per order, i.e. 1000 round trips per request, while `store.go:72` already exposed a batched `GetUsers(ids)` doing it in a single round trip.
3. Minor: `cleanNote` (`report.go:31-32` old) called `regexp.MustCompile` on every invocation (1000×/request) instead of compiling once at package init.

## Measurements

Command: `/tmp/claude/loadtest -url http://127.0.0.1:PORT/report -n 200 -c 20` (3 runs each), Go 1.27.1 darwin/arm64, same `NewStore(300,1000)` data, same machine, warm process.

| | concurrency=1 (p50/p95) | concurrency=20, n=200 (p50/p95, 3 runs) |
|---|---|---|
| **Before** | 141ms / 146ms | 2.83s/3.90s, 2.99s/4.24s, 2.84s/3.87s |
| **After** | 2ms / 5ms | 6ms/10ms, 5ms/8ms, 5ms/9ms |

At c=20, that's roughly **500x** lower p50 latency. The before/after numbers match the theory exactly: 20 requests × 141ms serial work ≈ 2.82s (observed baseline p50), confirming the mutex was serializing everything; removing it plus batching the user lookups collapsed per-request work from ~141ms to ~2ms of real CPU (regex + JSON encoding for 1000 lines). Also spot-checked at n=1000/c=50 after the fix: p50=11ms/p95=36ms, so it scales.

Existing test (`go test ./...`) still passes, and I diffed sample JSON output manually — no field or ordering差 change; batching preserves per-order behavior since `GetUsers` just resolves the same IDs in one round trip instead of N.

## Cost/trade-offs
- No added complexity — the fix is strictly simpler (fewer lines, no mutex, no regex churn) and uses a batching primitive the codebase already had, unused.
- The only behavior change: report generation is no longer globally serialized. If some *other* code relied on that mutex for reasons not visible in this file, removing it would need re-checking — I saw no such use.

## Housekeeping note
The sandbox denied me permission to `kill`/`pkill` processes, so a baseline test server I started on `127.0.0.1:8081` (pid 84196) and the fixed one on `127.0.0.1:8082` are still running in the background. Let me know if you'd like me to leave them, or if you can stop them yourself (`kill 84196` and the 8082 one) outside this sandbox.
