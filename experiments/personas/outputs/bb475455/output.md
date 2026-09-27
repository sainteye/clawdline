## Verdict: needs rework

Two of the blockers are security holes (SQL injection, unauthenticated delete), so this shouldn't ship as-is.

### Blockers

**1. SQL injection via `sort` parameter** — `store.go:100-105`, fed from `handler.go:107` (`q.Get("sort")`)
```go
if p.Sort != "" {
    q += " ORDER BY " + p.Sort
```
`Sort` comes straight from the query string with no allowlist, unlike `Status` which is checked against `allowedStatus` (store.go:82). A request like `GET /orders?sort=1);DROP TABLE orders;--` or a UNION-based payload goes straight into the query string. Needs a whitelist of allowed columns/directions, same pattern as `allowedStatus`.

**2. `DELETE /admin/orders/{id}` has no auth middleware** — `main.go:61`
```go
mux.HandleFunc("DELETE /admin/orders/{id}", h.deleteOrder)
```
Every other privileged route is wrapped (`requireAdmin(cfg.AdminToken, h.refundOrder)` at line 60, `requireUser` for the user routes). This one isn't wrapped in anything — any anonymous caller can permanently delete any order and its line items (`store.go:387-409` runs a real `DELETE` in a transaction). This looks like a missing `requireAdmin(cfg.AdminToken, ...)` wrapper.

**3. `OrderCache.Get` isn't synchronized** — `cache.go:30-36`
```go
func (c *OrderCache) Get(id int64) (Order, bool) {
	e, ok := c.entries[id]
```
`Set`/`Delete` both take `c.mu`, but `Get` reads the map with no lock at all. Any concurrent `Get` racing a `Set`/`Delete` (which will happen under real traffic — `getOrder` and `batchGetOrders` both call `Get` while other requests call `Set`) is a data race on a Go map, which can crash the process with `fatal error: concurrent map read and write`. `Get` needs to take the same mutex (`sync.RWMutex` would be a reasonable upgrade so hot-path reads don't fully serialize).

**4. Cache TTL is nanoseconds, not seconds** — `main.go:52`
```go
cache := NewOrderCache(time.Duration(cfg.CacheTTLSeconds))
```
`CacheTTLSeconds` is an int like `30`; casting it directly to `time.Duration` produces `30ns`, not 30 seconds. Every cache entry is expired essentially the instant it's written, so the whole feature this PR is built around never actually caches anything. Should be `time.Duration(cfg.CacheTTLSeconds) * time.Second`.

**5. Pagination offset is off by one page** — `store.go:106`
```go
args = append(args, p.PageSize, p.Page*p.PageSize)
```
`Page` is documented as 1-based (`ListParams.Page // 1-based`, store.go:71). With `page=1, page_size=20` this computes `OFFSET 20`, skipping the first 20 orders entirely — page 1 never shows the newest orders. Should be `(p.Page-1)*p.PageSize`.

### Should-fix

**`countRows` is never closed** — `store.go:90-98`. `QueryContext` returns a `*sql.Rows` that must be `Close()`d (directly or by draining) or the underlying connection stays checked out. Every call to `ListOrders` leaks a connection from the pool. Needs `defer countRows.Close()` right after the error check.

**Expired cache entries are never evicted** — `cache.go:30-48`. `Get` treats an expired entry as a miss but doesn't delete it, and there's no background sweep. Since orders are looked up by ever-growing IDs, the map only grows — it's an unbounded memory leak over the life of the process, not just a TTL correctness issue.

**No tests for any of the new behavior** — cache, list, batch-get, and delete are all new endpoints, and one of them is a destructive, currently-unauthenticated delete. Given the bugs above, this would have caught at least the offset and TTL issues.

### Nits

- `batchGetOrders` doesn't dedupe `in.IDs`; a caller passing the same id twice gets it twice in the response (cosmetic, not a security issue since output is still filtered by `uid`).

### Verified correct

- `getOrder`'s cache check re-validates `o.UserID == userID(r)` before trusting a cache hit (`handler.go:41`) — no cross-user auth bypass there.
- `batchGetOrders` gathers cache hits and DB misses first, then filters everything by `uid` at the very end (`handler.go:178-184`) — even though cache lookups aren't user-scoped, the final filter prevents leaking other users' orders.
- Goroutine loop in `batchGetOrders` (`handler.go:156-169`) closes over the per-iteration `i`/`chunk` — safe under Go 1.22's per-iteration loop-variable semantics (`go.mod` declares `go 1.22`), so no classic closure-capture bug.
- `GetOrdersByIDs` builds its `IN (...)` clause with real placeholders (`store.go:130-137`), not string concatenation — not injectable.
- Cache invalidation is wired correctly on `refundOrder` and `deleteOrder` (`handler.go:90`, `handler.go:197`).

### Questions

- Was `DELETE /admin/orders/{id}` meant to require `requireAdmin` like `refundOrder`, or is there a separate auth layer (e.g., a gateway) intended to gate `/admin/*` that just isn't reflected here?
- Is `sort` meant to accept arbitrary `column direction` combos, or a fixed small set (e.g. `created_at`, `amount_cents` × `asc`/`desc`)? That determines whether the fix is a strict allowlist of full strings or of column/direction parts.
