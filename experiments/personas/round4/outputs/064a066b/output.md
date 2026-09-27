## Summary

**What was measured:** end-to-end latency of `GET /report` over HTTP, using the repo's own `loadtest` tool, against a server built from source with `go build`, on localhost, backed by the default dataset (`NewStore(300, 1000)` — 300 users, 1000 orders).

**Bottlenecks found** (`report.go`, before):
1. **`reportMu sync.Mutex`** (report.go:28, old) wrapped the entire `BuildReport`, serializing *every* concurrent request even though the store never mutates (no write endpoints exist) — it bought no correctness and capped throughput at one report at a time.
2. **N+1 store calls**: `BuildReport` called `s.GetUser(o.UserID)` once per order (1000 sequential 100µs round trips) instead of the batch `s.GetUsers(ids)` the store already exposes (store.go:72).
3. `uniqueSorted` deduplicated with a linear scan per item — O(n²) over ~3000 tags + 1000 customer names.
4. `cleanNote` called `regexp.MustCompile` twice per order (2000 compiles per request) instead of once at package init.

**Baseline** (before, `go run ./loadtest`, `ADDR=127.0.0.1:19081`):
| load | wall | p50 | p95 |
|---|---|---|---|
| `-n 10 -c 1` | 1.336s | 133ms | 135ms |
| `-n 200 -c 8` (5 runs) | 27.6–30.4s | 1.08–1.14s | 1.16–1.66s |

**After** (same tool/flags, same dataset, `ADDR=127.0.0.1:19082`):
| load | wall | p50 | p95 |
|---|---|---|---|
| `-n 10 -c 1` | 20ms | 2ms | 4ms |
| `-n 200 -c 8` (5 runs) | 79–81ms | 3ms | 4ms |

That's a **~65x** drop in single-request latency and **~350x** drop in wall time under concurrency=8. Output verified byte-identical between old and new binaries via `diff` on the JSON response, and `go test ./...` still passes.

**Cost/trade-off:** none functionally — the mutex was protecting nothing (no writer exists), and the other three changes are drop-in equivalents with no API or behavior change. If a write path is ever added to the store, revisit whether `BuildReport` needs its own synchronization again.

**Not measured:** behavior with a real database (the store's 100µs sleep is a synthetic stand-in) — real network/query latency and connection-pool limits will dominate differently, so these numbers describe the app-layer fix, not a production capacity guarantee. I left the two idle test-server processes (ports 19081/19082) running since the sandbox wouldn't let me kill them; the kill command is above if you want them gone.
