Reviewed the diff against the full source. This PR adds a lot of value (list/pagination endpoint, batch fetch, order cache, hard delete) but has several real bugs, including two security-severity ones. Findings below are all verified against the actual code.

## Verdict: needs rework

## Blockers

1. **Unauthenticated hard-delete of any order.** `main.go:61` registers `mux.HandleFunc("DELETE /admin/orders/{id}", h.deleteOrder)` with no middleware at all, while the sibling admin route right above it is wrapped: `mux.HandleFunc("POST /admin/orders/{id}/refund", requireAdmin(cfg.AdminToken, h.refundOrder))` (`main.go:60`). Anyone can delete any order with zero credentials.

2. **SQL injection via `sort`.** `handler.go:107` passes `q.Get("sort")` straight into `ListParams.Sort`; `store.go:333-334` concatenates it into the query unsanitized: `q += " ORDER BY " + p.Sort`. Unlike `status`, which is checked against `allowedStatus` (`store.go:314-316`), `sort` has no allow-list — `GET /orders?sort=...` reaches the DB as raw SQL.

3. **Pagination off-by-one drops the real first page.** `store.go:338`: `args = append(args, p.PageSize, p.Page*p.PageSize)` computes `OFFSET = page*pageSize` instead of `(page-1)*pageSize`. For page 1 that's `OFFSET 20` (with default page_size), so the first 20 orders are skipped — and since `handler.go:98-100` clamps `page` to a minimum of 1, those rows are permanently unreachable through this endpoint.

4. **Data race in `OrderCache.Get`.** `cache.go:24-30` reads `c.entries` without taking `c.mu`, while `Set`/`Delete` (`cache.go:32-42`) do lock it. `getOrder` and `batchGetOrders` call `Get` on essentially every request while other requests call `Set`/`Delete` concurrently — this is a genuine Go data race on a map and can crash the process (`fatal error: concurrent map read and map write`) under real load.

5. **Cache TTL is 30 *nanoseconds*, not 30 seconds.** `main.go:52`: `NewOrderCache(time.Duration(cfg.CacheTTLSeconds))`. `time.Duration` is int64 nanoseconds, so `CacheTTLSeconds=30` gives a 30ns TTL — every entry is expired before the next request can ever observe it. The entire caching feature this PR adds never actually caches anything. Needs `time.Duration(cfg.CacheTTLSeconds) * time.Second`.

6. **DB connection leak on every list call.** `store.go:322-330` issues the count query with `QueryContext` and calls `.Next()` once but never `.Close()`s the `Rows` (no `defer`, unlike the second query at `store.go:345`). Under sustained traffic this leaks connections out of the pool. Use `s.db.QueryRowContext(...).Scan(&total)` instead.

## Should-fix

- `batchGetOrders` queries and caches orders for **any** requested ID regardless of ownership (`handler.go:145-182`) — `GetOrdersByIDs` has no `user_id` filter. The final loop does filter the *response* by `uid` (`handler.go:178-184`), so no data leaks, but the server needlessly fetches and caches other users' orders whenever a caller passes IDs it doesn't own. Scope the query by `UserID`, like `ListOrders` does.
- `OrderCache` never proactively evicts expired entries — they only get overwritten/deleted for the same key (`cache.go`). Long-running processes touching many distinct order IDs will grow the map unbounded.
- No tests were added for any of this PR's new surface (cache, `/orders` list+pagination+sort, `/orders/batch`, `DELETE /admin/orders/{id}`). A pagination test asserting page 1 returns the first N rows, or a `-race` test hitting `Get`/`Set` concurrently, would have caught findings #3 and #4.

## Nits

- Duplicate IDs in the `batchGetOrders` request body aren't deduped, causing redundant cache/DB lookups (harmless, bounded by `maxBatchIDs`).

## Verified correct

- `getOrder`'s cache path checks `o.UserID == userID(r)` before serving a cache hit, and only `cache.Set`s after confirming ownership (`handler.go:41`, `handler.go:55`) — no cross-user leak on the single-get path.
- `DeleteOrder` wraps both deletes in a transaction with `defer tx.Rollback()` and only commits after confirming `RowsAffected() > 0` (`store.go:387-408`) — no partial deletes.
- `status` filter is validated against `allowedStatus` before being interpolated (`store.go:314-316`) — safe from injection.
- go.mod pins Go 1.22, which has per-iteration loop variables, so the `go func(){...}()` closures over `i`/`chunk` in `batchGetOrders` (`handler.go:157-162`) are race-free despite not being passed as params.

## Questions

- Is the missing `requireAdmin` on `DELETE /admin/orders/{id}` intentional (e.g., meant to sit behind an internal-only network boundary), or an oversight? It's inconsistent with the only other `/admin/*` route.
- Should `sort` accept a validated enum (like `status` does) rather than arbitrary text?
