The sandbox blocks `kill`, so both test servers (ports 18400 and 18401, PIDs 66547 and 67869) are still running in the background — they're harmless loopback-only Go processes, but you'll want to kill them yourself from a normal terminal (`kill 66547 67869`) when convenient.

**Results: p50 latency dropped from ~1.06s to ~3ms (~350x faster).**

Fixes made in `report.go`:
1. **N+1 queries → batched fetch**: `BuildReport` was calling `s.GetUser(o.UserID)` once per order (1000 round trips at 100µs each = serialized delay). Now it collects distinct user IDs and does one `s.GetUsers(ids)` call.
2. **Removed the global `reportMu` mutex**: it serialized *every* concurrent `/report` request end-to-end for no reason — `Store` already has its own `RWMutex` protecting the actual data, and `BuildReport` only writes to local variables. This was the biggest win, since it queued up all concurrent requests behind one another.
3. **`uniqueSorted` was O(n²)**: it did a linear scan of the output slice for every item. Replaced with a map-based dedup, O(n).
4. **`cleanNote` recompiled two regexps on every single call**: hoisted `regexp.MustCompile` to package-level vars so they compile once.

Ran `go vet`, `go test ./...` (still passing), and the included load tester before/after to confirm the speedup.
