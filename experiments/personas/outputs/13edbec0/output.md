I reviewed the diff plus the full contents of the touched files (`cache.go`, `handler.go`, `main.go`, `store.go`). This PR adds an in-memory order cache, list/batch-get endpoints, and a delete endpoint. There are two critical issues and several real correctness bugs.

## Critical

**1. Unauthenticated order deletion — `main.go:61`**
```go
mux.HandleFunc("DELETE /admin/orders/{id}", h.deleteOrder)
```
Every other admin route wraps its handler in `requireAdmin(cfg.AdminToken, ...)` (see the refund route right above it). This one doesn't, and `deleteOrder` itself performs no auth check. Anyone who can reach this service can delete any order and its line items with no credentials at all.

**2. SQL injection via `sort` — `store.go:102`**
```go
q += " ORDER BY " + p.Sort
```
`p.Sort` comes straight from `q.Get("sort")` in `handler.go` with zero validation — unlike `Status`, which is checked against `allowedStatus` before use. This lets a caller inject arbitrary SQL into the query (blind/boolean injection via `ORDER BY` subqueries at minimum, more depending on driver behavior). Needs an allow-list, e.g. `map[string]string{"created_at": "created_at", "amount": "amount_cents"}` mapped to safe column/direction strings, same pattern as `allowedStatus`.

## Correctness bugs

**3. Cache TTL is nanoseconds, not seconds — `main.go:52`**
```go
cache := NewOrderCache(time.Duration(cfg.CacheTTLSeconds))
```
`cfg.CacheTTLSeconds` is a plain int (e.g. `30`); `time.Duration(30)` is 30 *nanoseconds*. Every cache entry expires effectively immediately, so the cache never actually caches anything under real traffic. Should be `time.Duration(cfg.CacheTTLSeconds) * time.Second`.

**4. Pagination offset is off by one page — `store.go:106`**
```go
args = append(args, p.PageSize, p.Page*p.PageSize)
```
`Page` is 1-based, so offset should be `(p.Page-1)*p.PageSize`. As written, page 1 (offset 20 for the default page size) skips the first page of results entirely, and every subsequent page is likewise shifted.

**5. Unsynchronized map read in the cache — `cache.go:31`**
```go
func (c *OrderCache) Get(id int64) (Order, bool) {
	e, ok := c.entries[id]   // no mu.Lock()
```
`Set`/`Delete` both take `c.mu`, but `Get` reads the map without any lock. Under concurrent requests (normal for an HTTP server) this is a data race on a Go map, which can crash the process with `fatal error: concurrent map read and map write` — not just a benign race, it's fatal and unrecoverable. `Get` needs the same mutex.

**6. `DeleteOrder` errors are swallowed but still return success — `handler.go:194-198`**
```go
if err := h.store.DeleteOrder(r.Context(), id); err != nil && !errors.Is(err, ErrNotFound) {
    log.Printf("delete order %d: %v", id, err)
}
h.cache.Delete(id)
w.WriteHeader(http.StatusNoContent)
```
Any non-`ErrNotFound` DB error (e.g. connection failure) is logged but the handler still responds `204 No Content`, telling the caller the delete succeeded when it may not have. Every other handler in this file returns 500 on unexpected errors — this one should too.

## Minor

**7. Unclosed `Rows` in the count query — `store.go:90-98`**
```go
countRows, err := s.db.QueryContext(ctx, `SELECT count(*) FROM orders`+where, args...)
...
if countRows.Next() { ... }
```
`countRows` is never closed (no `defer countRows.Close()`, and it's not drained to EOF). Under load this leaks connections back to the pool. Simplest fix: use `s.db.QueryRowContext(...).Scan(&total)` instead, since it's always exactly one row.

---
Priority-wise: fix #1 and #2 before this merges — they're exploitable today. #3–#6 are real bugs but not security holes (though #3 quietly defeats the PR's stated purpose, and #4 is a user-facing functional bug).
