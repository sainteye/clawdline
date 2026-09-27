I reviewed the diff (a Go wallet service adding payouts, transfers, a daily transfer limit, and money formatting). Found two critical bugs and several other real issues.

## Critical

**1. IDOR on `GET /transfers/{id}` — arbitrary transfer disclosure** (`main.go:424`, `transfer.go:1033-1045`)
The route reuses the `owned` middleware, which calls `requireOwner`. But `requireOwner` treats the path's `{id}` as an *account* ID (`s.store.GetAccount(r.Context(), id)`), while on this route `{id}` is actually the *transfer* ID. Two consequences:
- A user who happens to own account `#N` passes the ownership check for `GET /transfers/N`, even though it has nothing to do with that transfer.
- `getTransfer` itself never checks that the caller is the sender or receiver.

Net effect: any authenticated user who owns account `#1` can `GET /transfers/1` and see the amount/memo/counterparties of a transfer between two unrelated accounts, purely by ID coincidence (both tables likely start IDs at 1). This needs its own ownership check inside `getTransfer` against `t.FromAccountID`/`t.ToAccountID`, not the account middleware.

**2. `PayoutClient.Send` returns success after exhausting retries** (`payouts.go:637-669`)
```go
body, err := json.Marshal(...)          // outer err
...
for attempt := 1; attempt <= c.maxAttempts; attempt++ {
    resp, err := c.post(ctx, body, p.Reference)   // := shadows err inside the loop body
    ...
}
return PayoutResult{}, err   // refers to the OUTER err — still nil
```
`resp, err := c.post(...)` declares a *new*, loop-scoped `err` (legal because `resp` is new). Once retries are exhausted, the function falls out of the loop and returns the original outer `err`, which is still `nil` from the earlier `json.Marshal` call. So `Send` reports success (`nil` error, zero-value `PayoutResult`) even when every attempt failed. In `createPayout`, the account has already been debited via `Withdraw` before this call — since `err == nil`, the reversal branch is skipped, and the client gets `202 Accepted` with empty `provider_id`/`status` for a payout that was never actually accepted by the provider. Money leaves the ledger with no record it failed.

## High

**3. `lookupError` has no `Unwrap()`, breaking `errors.Is(err, ErrNotFound)`** (`store.go:728-737`, `handler.go:349-361`)
`GetAccount`/`GetTransfer` now wrap `ErrNotFound` in `&lookupError{...}`, which only implements `Error()`. Without `Unwrap()`, `errors.Is` can't see through it, so `writeStoreError`'s `errors.Is(err, ErrNotFound)` branch never matches for these two lookups — every "not found" becomes a 500 instead of a 404. It also undermines the anti-enumeration comment in `requireOwner`: a missing account now returns 500 while an existing-but-not-owned account returns 404, an observable difference where none was intended. Fix: add `func (e *lookupError) Unwrap() error { return e.err }`.

**4. `startOfBusinessDay` uses the UTC date, not the location's local date** (`config.go:133-136`)
```go
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.UTC().Date()          // should be now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```
For roughly the first `|UTC offset|` hours of each local day (e.g. 00:00–08:59 in Asia/Tokyo, or the last few hours before midnight in America/New_York), this computes the wrong calendar day, shifting the daily-limit window by 24h. `TestStartOfBusinessDay` only exercises times where the UTC and local dates happen to coincide (17:45 Tokyo, 18:00 Berlin, 9:30/12:00 NY), so it doesn't catch this.

## Medium

**5. Daily transfer limit is off by 100x (unit mismatch)** (`config.go:53-56`, `transfer.go:1058`)
`DailyTransferLimit` is documented and configured in whole currency units ("1000 means 1,000.00"), but `checkDailyLimit` compares it directly against `sent`/`amount`, which are in minor units (cents): `sent+amount > s.opts.DailyLimit`. With the default of `1000`, the real enforced cap is $10.00/day, not $1,000.00. Also doesn't account for currencies with different exponents (JPY=0, KWD=3).

**6. Goroutine leak in `NotifyWithin`** (`notify.go:552-561`, `transfer.go:1074-1079`)
`goResult` uses an unbuffered channel. If `waitOrTimeout` times out first (the whole point of `NotifyWithin`, default wait 300ms vs. 5s client timeout), nothing ever reads from that channel again, so the goroutine blocks forever on `ch <- f()`. Every webhook call slower than `NotifyWait` leaks a goroutine permanently. Fix: `make(chan error, 1)`.

**7. Wrong `Location` header after creating a transfer** (`transfer.go:1030` vs `main.go:424`)
`writeCreated(w, r.URL.JoinPath(id).Path, t)` builds `/accounts/{id}/transfers/{transferID}`, but the registered read route is the top-level `GET /transfers/{id}`. A client following the `Location` header gets a 404.

**8. Webhook `Summary` hardcodes 2 decimal places** (`notify.go:568`)
`fmt.Sprintf("... %.2f %s ...", float64(ev.AmountCents)/100, ev.Currency, ...)` ignores per-currency exponents that `money.go` (added in this same PR) already models correctly — JPY and KWD summaries will be wrong (e.g. a 1250-minor-unit KWD amount shows as "12.50" instead of "1.250"). Should reuse `FormatAmount`.

## Worth a design note

**9. Check-then-act races on balance and daily limit.** `createTransfer`/`createPayout` check `BalanceCents` and `checkDailyLimit` against previously-loaded/queried values, then debit in a separate step; `adjustBalance` has no floor guard (`balance_cents + ? >= 0`). Concurrent requests against the same account can both pass the checks and overdraw or blow past the daily limit. Worth an atomic conditional update rather than read-then-write.

Everything else — the `writeStoreError` consolidation, `Credit`/`Withdraw`/`CreateTransfer` transaction structure, retry/backoff design, and `FormatAmount`/tests — looked solid.
