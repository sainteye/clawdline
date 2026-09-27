All changes verified: `go vet`/`go test` pass, and the response is byte-for-byte identical (SHA-256 `859f20f2…` before and after). Here's the full picture.

## What was slow, and why

**`report.go`** built the report by calling `s.GetUser(o.UserID)` once per order in a loop — an N+1 pattern. With 1000 seeded orders, that's ~1000 sequential round trips at `queryLatency` = 100µs each (`store.go:10`), i.e. ~100ms of pure simulated I/O per report, even though the store already exposed a batched `GetUsers(ids)` (`store.go:72`) that nothing used.

On top of that, `BuildReport` was wrapped in a package-level `reportMu sync.Mutex` (old `report.go:27-28`) that serialized *every* concurrent request globally — even though `BuildReport` only reads store data that the store already protects internally with its own `RWMutex`. There was no shared mutable state left to protect, so this lock turned all concurrent requests into a queue.

Two smaller CPU costs: `cleanNote` called `regexp.MustCompile` twice per order (2000 compiles/report), and `uniqueSorted` deduplicated via an O(n²) nested-loop scan.

## What I changed (`report.go`)

1. Removed `reportMu` — nothing it protected was actually shared/mutable.
2. Replaced the per-order `GetUser` loop with one `GetUsers(ids)` call using deduped IDs collected up front.
3. Hoisted the two regexps to package-level vars, compiled once.
4. Rewrote `uniqueSorted` to dedup with a map instead of a nested loop.

## Evidence

**Byte-for-byte check**: `curl -s http://.../report -o body.json` before and after, `shasum -a 256` → identical (`859f20f2…`) both times; `diff` reported no differences.

**HTTP load test**, `go run ./loadtest -n 100 -c 20` against `go run .` (store `NewStore(300,1000)`, default in `main.go`):

| | wall | p50 | p95 |
|---|---|---|---|
| before (run 1) | 13.748s | 2.728s | 2.833s |
| before (run 2) | 14.780s | 2.722s | 3.865s |
| **after (run 1)** | **35ms** | **6ms** | **11ms** |
| **after (run 2)** | **36ms** | **7ms** | **11ms** |
| after, heavier (`-n 500 -c 100`) | 119ms | 22ms | 52ms |

Single request (`curl`, one-shot): ~142ms before → ~10ms after.

**CPU-only benchmark** (`report_bench_test.go`, isolates `BuildReport` from HTTP), `go test -bench BenchmarkBuildReportParallel -benchtime 20x -cpu 20 -run '^$' .`: before **133,147,973 ns/op**; after (5 runs, `-benchtime 50x -count 5`): **236,893–290,538 ns/op** (median ~251µs) — ~500x.

**Which change did what:**
- *Batched `GetUsers`* is the dominant term: it explains the ~14x single-request drop (142ms→10ms), since eliminating ~999 round trips removes ~99.8ms directly.
- *Mutex removal* is what let concurrency actually help: `go test -bench BenchmarkBuildReportParallel -cpu 1,20` on the new code gives cpu=1 → 1,391,098 ns/op vs cpu=20 → 275,451 ns/op — per-op cost *drops* under concurrency, which is only possible once requests stop serializing on a lock. This also explains why the load-test p50 improved ~450x while the single-request case only improved ~14x — the extra gain is concurrency, not raw speed.
- *Precompiled regexps*: isolated micro-bench over 1000 notes, old loop 1.60ms → new 0.99ms (~1.6x for that step).
- *Map-based `uniqueSorted`*: on customer-shaped data (300 unique/1000, matching the seed data), old 203µs → new 30.8µs (~6.6x). Honest caveat: on tag-shaped data (only 8 unique/3000), the map version is actually slightly *slower* (12.6µs → 23.25µs) since a tiny linear scan beats hashing when the output stays minuscule — net effect across a real report is still a clear win (~172µs saved vs ~11µs lost), so I kept the map version.

## Not changed
- `store.go` (`queryLatency`, seed data) and the HTTP API/response shape — out of scope.
- Did not special-case `uniqueSorted` for the tags call to claw back that ~11µs; not worth the complexity.
- Did not add response caching. The store never mutates after `NewStore`, so caching the whole report would be safe today and would beat all of the above, but nothing in the code guarantees the store stays static — that's a bigger, more invasive change than this task needed, so I left it as a suggestion rather than implementing it.

One note: I couldn't stop the two throwaway dev servers I started for testing (`127.0.0.1:18391` old code, `127.0.0.1:18493` new code, PIDs 83590/90616) — `kill` was refused (`operation not permitted`), since each Bash sandbox invocation can't signal another invocation's processes. They're harmless localhost-only leftovers; you can close them from your own terminal/session if you'd like them gone.
