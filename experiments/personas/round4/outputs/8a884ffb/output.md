## Summary

**What was slow and why**

`GET /report` had three problems in `report.go`, compounding under load:

1. **`reportMu sync.Mutex` serialized every request.** `BuildReport` took a global lock for its entire duration, so concurrent requests queued up one after another even though nothing in the function actually needed cross-request synchronization (the store already protects its own state with an `RWMutex`, and `BuildReport` only touches local variables).
2. **N+1 store round trips.** For each of the 1000 orders it called `s.GetUser(o.UserID)` individually — 1000 sequential 100µs sleeps (`queryLatency`) instead of one batched `s.GetUsers(ids)` call.
3. **Per-call regex compilation.** `cleanNote` called `regexp.MustCompile` twice on every single order line.
4. Minor: `uniqueSorted` deduplicated with an O(n²) nested-loop scan instead of a map.

Under concurrency, #1 dominated: latency scaled almost linearly with concurrency and throughput stayed flat (~7.3 req/s) no matter how many clients hit it at once.

**What I changed** (`report.go`): removed `reportMu`; replaced per-order `GetUser` calls with one `GetUsers(ids)` batch call after collecting unique user IDs from the listed orders; hoisted the two regexes to package-level `var`s compiled once; replaced the O(n²) dedup with a map-based one. No changes to `store.go`, `main.go`, the API, or response shape.

**Measurements** (`go run .` + `go run ./loadtest -n 64 -c <N>`, same seed data both times):

| concurrency | before (wall / p50 / p95) | after (wall / p50 / p95) |
|---|---|---|
| 1  | 8.745s / 135ms / 150ms | 121ms / 2ms / 2ms |
| 8  | 8.707s / 1.08s / 1.109s | 51ms / 5ms / 15ms |
| 32 | 10.772s / 5.064s / 5.472s | 46ms / 21ms / 39ms |

Larger sample at c=32: `n=256` → wall=71ms, p50=8ms, p95=16ms (before: throughput was flat ~7.3 req/s regardless of concurrency; after, it scales).

- Removing `reportMu` is what let throughput actually scale with concurrency instead of staying flat — visible in wall time barely increasing (121ms→51ms→46ms) as concurrency rises from 1→8→32, versus before where wall time only got worse (8.7s→8.7s→10.8s).
- Batching `GetUser`→`GetUsers` is what dropped single-request latency from ~135ms to ~2ms (1000 round trips → 2 round trips per request).
- Precompiling regexes and the map-based dedup are smaller wins layered on top, avoiding wasted CPU per request now that requests run concurrently and cheaply.

**Correctness check:** `curl` of `/report` before and after produLce the same MD5 (`540b969e5e4391a521fe748f12c38846`) and `diff` shows no difference. Also ran the concurrent load test against a `-race`-built binary (`go build -race`) — no race reported, confirming removing the mutex didn't introduce a data race. `go vet ./...` and `go test ./...` both pass.

**Left unchanged:** `queryLatency`, seed data, and the API/response format, as required. Didn't touch `GetUser` (still used by nothing now, but left in `store.go` as part of the store's public API/scope boundary) or add caching, since the store's round-trip cost is the given cost model, not something to bypass beyond batching what already has a batch API.

**Note:** several background test server processes I started during measurement (`go run .` on ports 8081, 18415, 18417, 18418, 18391, 19081, 18493) are still running — the sandbox doesn't let me signal processes across separate Bash invocations. They're harmless local test servers, but let me know if you'd like the exact `kill` commands to clean them up yourself.
