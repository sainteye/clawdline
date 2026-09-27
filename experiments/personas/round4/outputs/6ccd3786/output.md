Fixed two bottlenecks in `report.go`:

1. **N+1 queries** — `BuildReport` called `s.GetUser(o.UserID)` once per order (1000 round trips × 100µs each ≈ 100ms). Replaced with a single batched `s.GetUsers(ids)` call.
2. **Global mutex** — `reportMu` serialized *all* concurrent `/report` requests even though it only guarded a local variable, so concurrent load queued up completely (p50 was 5.53s under 8 concurrent requests). Removed it since there's no shared state to protect.
3. Also replaced the O(n²) nested-loop dedup in `uniqueSorted` with a map-based O(n) version.

Results (`loadtest -n 20 -c 8`): p50 5.53s → 6ms, p95 6.86s → 10ms, wall 16s → 20ms. Verified output is byte-identical to the old implementation and `go test ./...` passes.
