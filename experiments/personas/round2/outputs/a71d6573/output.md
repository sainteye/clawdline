I read through `pr.diff` (a Go wallet service adding transfers, payouts, currency formatting, and a daily transfer limit). Found several real bugs, ranked by severity — the top two are financial-integrity/security issues, not style nits.

## Critical

**1. `CreateTransfer`'s commit/rollback decision is defeated by variable shadowing — a transfer can commit without moving any money.** (`store.go`)

```go
defer func() {
    if err != nil { _ = tx.Rollback(); return }
    err = tx.Commit()
}()
...
if err := adjustBalance(ctx, tx, t.FromAccountID, -t.AmountCents); err != nil {
    return Transfer{}, fmt.Errorf("debit account %d: %w", t.FromAccountID, err)
}
```

`if err := ...; err != nil` inside the `if` declares a **new**, block-scoped `err` — it never touches the outer `err` the deferred closure inspects. So if `adjustBalance` (either side) or `insertEntry` fails after the initial `INSERT INTO transfers` succeeds, the function returns an error to the caller, but the outer `err` is still `nil` from the earlier `LastInsertId()` call, so the `defer` calls `tx.Commit()` anyway. Result: a `transfers` row gets committed with no matching balance change or ledger entries — a phantom transfer, or a debit with no matching credit, depending on where it fails. `Credit`/`Withdraw` right above it use the correct, boring `defer tx.Rollback()` pattern — `CreateTransfer` should do the same instead of the manual `err`-checking defer.

**2. `GET /transfers/{id}` is protected by the wrong authorization check.** (`main.go` + `auth.go`)

```go
mux.Handle("GET /transfers/{id}", owned(s.getTransfer))
```

`owned` = `s.authenticate(s.requireOwner(h))`, and `requireOwner` unconditionally does `id := pathID(r)` then `s.store.GetAccount(ctx, id)` and checks `acct.OwnerID == caller`. For every other route `{id}` is an *account* ID; for this route it's a *transfer* ID. So the middleware looks up an account whose ID happens to numerically equal the transfer ID — an unrelated entity. Consequences:
- Normal case: no account shares that ID, so legitimate transfer lookups 404 for everyone, including the sender/receiver — the endpoint is effectively broken.
- Worse case: if a transfer ID collides with an account ID the caller *does* own, they pass the check and see a transfer they had no part in (amount, memo, counterparty account IDs).

There's no check anywhere that the caller is actually the `FromAccountID` or `ToAccountID` owner of the transfer. This needs its own authorization function based on `GetTransfer`, not a reuse of the accounts middleware.

## Correctness bugs

**3. `lookupError` doesn't implement `Unwrap()`, so `errors.Is(err, ErrNotFound)` silently stops working.** (`store.go`)

```go
type lookupError struct{ what string; id int64; err error }
func (e *lookupError) Error() string { ... }
```

No `Unwrap() error`. Every `writeStoreError` call (deposits, payouts, transfers, `requireOwner`, `getTransfer`) checks `errors.Is(err, ErrNotFound)`, which now returns `false` for every account/transfer lookup miss, since `errors.Is` can't see through a wrapper with no `Unwrap`/`Is`. Every "not found" case now falls into the `default` branch → logged as an error and returned as 500. It also reopens the exact account-enumeration hole the code comments say they're avoiding: a nonexistent account now answers 500 while an existing-but-not-yours account answers 404 — distinguishable again. Fix: add `func (e *lookupError) Unwrap() error { return e.err }`.

**4. `NotifyWithin` leaks a goroutine on every timeout — exactly the case it exists to handle.** (`notify.go`)

```go
func goResult(f func() error) <-chan error {
    ch := make(chan error) // unbuffered
    go func() { ch <- f() }()
    return ch
}
```

If `waitOrTimeout` times out first (a slow webhook), nothing ever reads from `ch` again, so the background goroutine blocks forever on `ch <- f()`. Under a consistently slow/unreachable webhook this leaks one goroutine per request indefinitely. Fix: `make(chan error, 1)`.

**5. Daily transfer limit is off by 100x due to a units mismatch.** (`config.go` + `transfer.go`)

```go
// DailyTransferLimit ... in whole currency units (1000 means 1,000.00).
DailyTransferLimit int64
```
```go
if sent+amount > s.opts.DailyLimit { return ErrDailyLimit }
```

`sent` and `amount` are minor units (cents), but the config comment says the limit is in whole units. The default `1000` is documented as "$1,000.00/day" but is actually enforced as 1000 *cents* = $10/day. Separately, a single limit expressed in "whole units" is ambiguous once you have JPY (0 decimals) and KWD (3 decimals) accounts sharing the same config value.

**6. `startOfBusinessDay` takes the calendar date from UTC, not from the target location — off by a day for part of every day.** (`config.go`)

```go
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
    y, m, d := now.UTC().Date()
    return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```

This should be `now.In(loc).Date()`. As written, for Asia/Tokyo (UTC+9), any call between **00:00–08:59 JST** has a UTC date that's still the *previous* day, so this returns midnight of the wrong day — a full 24 hours early. The new test (`TestStartOfBusinessDay`) only exercises 17:45 JST, 9:30/12:00 America/New_York, etc. — all safely away from the boundary — so it passes despite the bug.

**7. Webhook `Summary` hardcodes 2-decimal formatting, mis-rendering JPY/KWD.** (`notify.go`)

```go
ev.Summary = fmt.Sprintf("%s of %.2f %s on account %d", ev.Type, float64(ev.AmountCents)/100, ev.Currency, ev.AccountID)
```

`FormatAmount` (added in this same PR, in `money.go`) already handles per-currency exponents correctly — this should call it instead. As written, a 500-yen deposit summarizes as "5.00 JPY".

## Worth a second look

- **Balance/limit checks race with the actual debit.** `createPayout`/`createTransfer` check `BalanceCents` and the daily limit in Go, then debit in a separate DB call with no `WHERE balance_cents >= ?` guard in `adjustBalance`. Two concurrent requests on the same account can both pass the check and overdraw it.
- **Payouts bypass `DailyTransferLimit` entirely** — only `createTransfer` calls `checkDailyLimit`; `createPayout` never does, so if the limit is meant to cap money leaving an account per day, payouts are an unguarded escape hatch.
- **Payout reversal assumes every `Send` error means "provider didn't do it."** `readPayout` can fail after a non-retryable 2xx (e.g. malformed JSON body) — that path still triggers `createPayout`'s "credit the money back" reversal even though the provider may have already accepted the payout.
- **Wrong `Location` header on `createTransfer`.** It builds `r.URL.JoinPath(id)` off `/accounts/{id}/transfers`, giving `/accounts/{id}/transfers/{transferID}`, but the actual registered read route is the top-level `/transfers/{id}`.

Nice touches worth calling out: the retry/backoff + `Retry-After` handling and idempotency key in `payouts.go`, and centralizing error-to-status mapping in `writeStoreError`. The bugs above are concentrated in the newer transfer/payout/business-day code, not the existing deposit path.
