I read through the diff and the checked-out source. This PR adds transfers, payouts, currency formatting, and a daily-transfer-limit — solid feature set with good bones (double-entry ledger, idempotency keys on payouts, config validation), but I found two critical financial-correctness bugs and several others worth fixing before this ships.

## Critical

**1. `payouts.go:71-93` — shadowed `err` swallows exhausted-retry failures**
```go
for attempt := 1; attempt <= c.maxAttempts; attempt++ {
    resp, err := c.post(ctx, body, p.Reference)   // shadows the outer err
    ...
}
return PayoutResult{}, err   // this is the OUTER err — still nil
```
`err` from `json.Marshal` (line 61) is `nil` by the time the loop starts. `resp, err := c.post(...)` inside the loop declares a *new* `err` scoped to the loop body, so nothing that happens on failed attempts ever touches the outer `err`. When every attempt is exhausted (e.g. the provider is down), `Send` returns `(PayoutResult{}, nil)` — a fake success with empty `ProviderID`/`Status`. In `handler.go:166-179`, `err == nil` skips the reversal-and-502 path entirely: the customer's balance was already debited by `Withdraw` (line 160), and the client gets `202 Accepted` with blank provider fields for a payout that was never confirmed.

**2. `store.go:191-233` — `CreateTransfer` can commit a half-applied transfer**
```go
defer func() {
    if err != nil { _ = tx.Rollback(); return }
    err = tx.Commit()
}()
...
if err := adjustBalance(ctx, tx, t.FromAccountID, -t.AmountCents); err != nil {   // shadows outer err
    return Transfer{}, fmt.Errorf(...)
}
if err := adjustBalance(ctx, tx, t.ToAccountID, t.AmountCents); err != nil {      // shadows outer err
    return Transfer{}, fmt.Errorf(...)
}
...
return t, nil   // hardcoded nil, discards defer's err = tx.Commit()
```
Every `if err := ...; err != nil` here is `if`-scoped, so it shadows the function-level `err` the defer inspects. If the credit to the destination fails after the debit from the source already succeeded, the outer `err` is still `nil` when the defer runs — it commits instead of rolling back, leaving the source debited with no matching credit or ledger entries. Separately, even if that were fixed, the return type is unnamed (`(Transfer, error)`) and the success path is `return t, nil`, so a failing `tx.Commit()` is silently discarded regardless. Compare with `Credit`/`Withdraw` right above (144-187), which do this correctly: `defer tx.Rollback()` unconditionally, plus explicit `return e, tx.Commit()`. `CreateTransfer` should use the same shape.

## High

**3. `store.go:19-27` — `lookupError` has no `Unwrap()`**, so `errors.Is(err, ErrNotFound)` in `writeStoreError` (handler.go:252) can't see through it. Every "account/transfer not found" from `GetAccount`/`GetTransfer` now falls into the `default` branch — logged as unexpected and returned as `500` instead of `404`. This also reopens the exact enumeration hole `requireOwner`'s comment (auth.go:60) says is closed: "account doesn't exist" now returns 500 while "account exists but isn't yours" returns 404 — an attacker can tell the two apart again. Fix: add `func (e *lookupError) Unwrap() error { return e.err }`.

**4. `config.go:98-101` — `startOfBusinessDay` uses the wrong timezone for the date**
```go
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
    y, m, d := now.UTC().Date()   // should be now.In(loc).Date()
    return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```
For the default `America/New_York` (behind UTC), during the last several hours of each local day (roughly 7–8pm–midnight, once UTC has already rolled to the next date), this returns a timestamp *after* `now`. `checkDailyLimit` (transfer.go:93-94) then queries `SentSince` with a `since` in the future, which matches zero rows, so `sent` is always `0` — the daily limit is effectively disabled for that window every day. `TestStartOfBusinessDay` doesn't catch it because none of its five cases land on that boundary.

## Medium

**5. `notify.go:41` + `transfer.go:115-119` — goroutine leak.** `goResult` uses an unbuffered channel; `waitOrTimeout` (handler.go:200-209) abandons it once the timer fires. Any webhook call slower than `NotifyWait` (default 300ms, shorter than most real HTTP round trips) leaves the spawned goroutine blocked forever on `ch <- f()` with no receiver. This will leak steadily in normal operation. Fix: `ch := make(chan error, 1)`.

**6. TOCTOU overdraft risk** — `createPayout` (handler.go:150) and `createTransfer` (transfer.go:46) check `acct.BalanceCents` loaded earlier in the request, but `adjustBalance` (store.go:250-264) blindly applies the delta with no floor check. Two concurrent requests can each pass the check and both apply, overdrawing the account — worse for payouts since that money actually leaves to a bank. Consider `UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ? AND balance_cents + ? >= 0` and treating 0 rows affected as insufficient funds.

**7. Same TOCTOU class for `checkDailyLimit`** (transfer.go:89-102) — it's a separate read outside the transaction that records the transfer, so concurrent transfers can jointly exceed the daily cap.

**8. `payouts.go:84-90`** — when `ctx.Done()` fires during the retry backoff, the function returns without closing `resp.Body`, leaking the connection.

## Low / worth a comment

- `payouts.go:41,62` — `providerPayout.AmountMinor` is `int32` filled from an `int64`. Safe today only because `maxPayoutCents` (25M) is under `math.MaxInt32`; fragile if that cap ever changes.
- `SentSince` (store.go:134) only sums `kind = "transfer_out"`, so payouts don't count against `DailyTransferLimit` — confirm that's intentional given the field's doc comment says "the most one account may send... per business day."

Want me to draft fixes for the two critical items (the payout retry loop and `CreateTransfer`), since those are the ones with direct money-safety impact?
