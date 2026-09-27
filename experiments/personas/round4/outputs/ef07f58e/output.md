This all looks correct. `store.go` and `main.go` were untouched (no API/seed/latency changes). Let me leave the "after" server running in case the user wants to poke at it, and provide the final report.

## Report

**What was slow and why**

`BuildReport` in `report.go` had three issues, all compounding under load:

1. **`reportMu sync.Mutex` fully serialized every `/report` request.** Since the store's own data never mutates after startup, this lock added no correctness benefit — it just meant only one report could be built at a time, no matter how much concurrency the load generator threw at it.
2. **N+1 store round trips.** The loop called `s.GetUser(o.UserID)` once per order (1000 calls × `queryLatency` 100µs ≈ 100ms) instead of using the store's existing batch method `GetUsers(ids)`, which does it in a single round trip.
3. **Regexes recompiled per order.** `cleanNote` called `regexp.MustCompile` twice *inside* the per-order loop instead of once at package scope.
4. **O(n²) dedup.** `uniqueSorted` used a linear scan to check "seen" instead of a map, costing ~4.5M comparisons for tags with 1000 orders.

**What I changed** (`report.go` only; `store.go`, `main.go`, API, and seed data untouched):
- Removed `reportMu` (store's own `RWMutex` already protects its data; `BuildReport` has no shared mutable state).
- Batch-fetch users via `s.GetUsers(ids)` instead of one `GetUser` call per order.
- Hoisted the two regexps to package-level `var`s, compiled once.
- Rewrote `uniqueSorted` to use a `map[string]struct{}` for O(1) membership checks, still sorting at the end (so output order/content is unaffected) and preserving nil-vs-empty slice semantics for JSON encoding.

**Correctness**: response bytes are identical — `md5` of `/report` before and after match (`540b969e5e4391a521fe748f12c38846`), confirmed with `diff`. `go vet ./...` and `go test ./... -race` pass.

**Measurements** (same seed: 300 users, 1000 orders; `go run ./loadtest`)

| Test | Before | After |
|---|---|---|
| Single request (`curl` timing) | ~148ms | ~10ms |
| `-n 40 -c 8` | wall 5.538s, p50 1.083s, p95 1.211s | wall 25ms, p50 4ms, p95 7ms |
| `-n 80 -c 16` | wall 10.936s, p50 2.162s, p95 2.223s | wall 32ms, p50 5ms, p95 9ms |
| `-n 400 -c 32` | not run (would be ~56s+ at baseline's serialized rate) | wall 223ms, p50 12ms, p95 48ms |

Which change helped what:
- Removing `reportMu` is what let concurrency actually overlap — baseline's wall time scaled linearly with request count regardless of concurrency (proof the requests were fully serialized); after removal, wall time for 80 requests at concurrency 16 is ~32ms instead of ~11s.
- Batching `GetUsers` is what cut single-request latency by ~15x (148ms → ~10ms), since it collapses 1000 sequential 100µs round trips into one.
- The regexp and dedup fixes are minor relative to the round-trip savings but remove needless CPU work per request, helping p95 tail latency under concurrent CPU contention.

**What I chose not to change**: didn't touch `store.go`'s `queryLatency`, locking, or seed data (out of scope), and didn't add caching/memoization of the report — the task explicitly requires the same data to always be reflected, and batching already gets round trips down to the minimum (2 per request: one `ListOrders`, one `GetUsers`), so caching would add complexity without a clear win given the current constraints.

I left the "after" build running on `127.0.0.1:18411` for you to inspect; the original baseline binary is still running on `127.0.0.1:18410` (the sandbox wouldn't let me signal/kill background processes started in earlier commands) — let me know if you'd like me to try harder to stop it, otherwise it's idle and harmless.
