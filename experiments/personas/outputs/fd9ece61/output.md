I reviewed the diff and cross-referenced it against the full files. There are two critical issues and several real bugs.

## Critical

**1. `DELETE /admin/orders/{id}` has no auth check at all** (`main.go:61`)
```go
mux.HandleFunc("DELETE /admin/orders/{id}", h.deleteOrder)
```
Every other admin route wraps with `requireAdmin(cfg.AdminToken, ...)` (see the refund route right above it). This one doesn't, and `deleteOrder` itself never calls `userID(r)` or checks anything — so any unauthenticated caller can delete any order and its line items. This looks like a copy-paste omission and needs to be `requireAdmin(cfg.AdminToken, h.deleteOrder)`.

**2. SQL injection via the `sort` query param** (`store.go:100-107`, `ListOrders`)
```go
if p.Sort != "" {
    q += " ORDER BY " + p.Sort
```
`Status` is validated against `allowedStatus`, but `Sort` (raw from `q.Get("sort")`) is concatenated straight into the query with no whitelist. That's an injection point (e.g. blind boolean/time-based via a subquery in `ORDER BY`). Needs a whitelist of allowed columns/directions, same pattern as `allowedStatus`.

## Bugs

**3. `OrderCache.Get` isn't synchronized** (`cache.go:24-30`)
`Set`/`Delete` take `c.mu.Lock()`, but `Get` reads `c.entries[id]` unlocked. That's a concurrent map read/write — under real traffic this will trip `fatal error: concurrent map read and map write` (or worse under `-race`).

**4. Pagination offset is off by one page** (`store.go:106`)
```go
args = append(args, p.PageSize, p.Page*p.PageSize)
```
`Page` is documented as 1-based, so page 1 should have offset 0, but this computes `1*PageSize`, skipping the entire first page. Should be `(p.Page-1) * p.PageSize`.

**5. Cache TTL unit bug** (`main.go:52`)
```go
cache := NewOrderCache(time.Duration(cfg.CacheTTLSeconds))
```
`time.Duration` is nanoseconds, so `CacheTTLSeconds: 30` becomes a 30ns TTL, not 30s. Entries expire almost immediately, defeating the entire point of the cache. Needs `time.Duration(cfg.CacheTTLSeconds) * time.Second`.

**6. `deleteOrder` returns success even when the delete failed** (`handler.go:194-199`)
```go
if err := h.store.DeleteOrder(r.Context(), id); err != nil && !errors.Is(err, ErrNotFound) {
    log.Printf("delete order %d: %v", id, err)
}
h.cache.Delete(id)
w.WriteHeader(http.StatusNoContent)
```
A real DB error is logged but swallowed — the handler still responds 204. Every other handler in this file returns 500 on unexpected errors; this one should too.

## Minor

**7. `countRows` is never closed** (`store.go`, `ListOrders`) — `s.db.QueryContext(...)` for the count leaks the `*sql.Rows`/connection on every call. Since you only expect one row, use `QueryRowContext` instead, or `defer countRows.Close()`.

**8.** `Page` has no upper bound, so a huge `page` value produces a huge `OFFSET` — not incorrect, just a potential performance footgun worth capping.

The batch endpoint's cache-then-DB-then-filter-by-owner logic (`handler.go:130-186`) and the parallel chunk fetching look correct, and since go.mod targets Go 1.22 the per-goroutine loop variable capture in `batchGetOrders` is fine.
