I reviewed the diff against the full source (not just the patch) and traced the call paths for the new transfer/payout/daily-limit code. Verdict: **needs rework** — several blockers, most in code this PR adds.

## Blockers

**1. `CreateTransfer` silently drops commit failures — client can be told "success" when nothing was saved.**
`store.go:191-232`. The function's signature is `(Transfer, error)` (not named returns), but the commit/rollback is done via `defer func() { ... err = tx.Commit() }()` at `store.go:196-202`, and the success path is a literal `return t, nil` at `store.go:232`. Since `err` is a plain local, assigning to it inside the deferred closure never reaches the already-fixed return values — if `tx.Commit()` fails, the function still returns `(t, nil)`. Compare with `Credit` (`store.go:163`, `return e, tx.Commit()`) and `Withdraw` (`store.go:186`), which return the commit result directly and are correct. Fix: either name the returns (`func (s *Store) CreateTransfer(...) (result Transfer, err error)`) so the defer actually mutates them, or drop the defer pattern here and do the same `return t, tx.Commit()` as the other two methods.

**2. `GET /transfers/{id}` is authorized against the wrong resource — broken for legitimate users, and an IDOR for the unlucky/lucky collision case.**
`main.go:70` wires this route through `owned(...)` → `requireOwner` (`auth.go:47-67`), which does `id, _ := pathID(r); acct, _ := s.store.GetAccount(ctx, id)` and checks `acct.OwnerID`. But for this route `{id}` is a **transfer** id, not an account id (`transfer.go:73-85`, `pathID` reads the same path segment again inside `getTransfer`). Since transfer ids and account ids are independent auto-increment sequences, in normal operation `GetAccount(transferID)` will almost always miss, so a user can never fetch their own transfer. Worse, `getTransfer` never checks that the caller is a party to the transfer (`from_account_id`/`to_account_id`) — if a transfer id happens to equal an account id the caller *does* own, `requireOwner` passes and `getTransfer` returns that transfer's full detail (amount, memo, both account ids) regardless of who was actually involved. Fix: `getTransfer` needs its own authorization — load the transfer, then verify the caller owns `from_account_id` or `to_account_id` — and shouldn't reuse `requireOwner` at all.

**3. No atomic floor check on debits — concurrent transfers/payouts can overdraw an account.**
`adjustBalance` (`store.go:250-264`) does `UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ?` with no `balance_cents >= -delta` guard. The only insufficient-funds checks are in the handlers (`transfer.go:46`, `handler.go:150`) against an `Account` snapshot loaded before the DB transaction even opens. Two concurrent transfers/payouts from the same account can both pass the pre-check and both debit, overdrawing the balance. Fix: make the debit conditional, e.g. `UPDATE accounts SET balance_cents = balance_cents - ? WHERE id = ? AND balance_cents >= ?`, and treat `RowsAffected == 0` as insufficient funds (distinguishing from not-found by checking existence separately or via the not-found path already in place).

**4. `startOfBusinessDay` uses the UTC calendar date, not the business location's — silently disables the daily limit for hours every day in the shipped default timezone.**
`config.go:97-101`: `y, m, d := now.UTC().Date(); return time.Date(y, m, d, 0,0,0,0, loc)`. For a zone behind UTC (default is `America/New_York`, `config.go:81`), e.g. now = 2026-03-14 21:00 EDT = 2026-03-15 01:00 UTC: `now.UTC().Date()` returns `2026-03-15`, so the computed start-of-day is `2026-03-15 00:00 EDT` = `2026-03-15 04:00 UTC` — 3 hours **after** `now`. `checkDailyLimit` (`transfer.go:93`) then queries `SentSince(ctx, accountID, since)` with a `since` in the future, which always returns 0 sent, so the daily limit is effectively off for that window every single day. None of the five cases in `TestStartOfBusinessDay` (`money_test.go:27-50`) cross a UTC/local day boundary, so this wasn't caught. Fix: `y, m, d := now.In(loc).Date()`.

**5. `DailyTransferLimit` is documented in whole currency units but compared against cents — the shipped default enforces $10/day, not $1,000/day.**
`config.go:25-28` says "in whole currency units (1000 means 1,000.00)"; `config.go:47` sets the default to `1000`. But `checkDailyLimit` (`transfer.go:98`) does `sent+amount > s.opts.DailyLimit` where both `sent` (`store.go:129-140`, sums `amount_cents`) and `amount` (`req.AmountCents`) are in cents. So the default limit is actually 1000 cents = $10.00/day, not $1,000.00. With this default, essentially any real transfer over $10 gets rejected with "daily transfer limit reached." Fix: either scale `DailyTransferLimit` to minor units when loading config, or fix the doc comment and default to match cents — but pick one and make the arithmetic match it.

**6. `NotifyWithin` leaks a goroutine on every webhook call slower than `NotifyWait`.**
`notify.go:34-43` runs `n.Notify` on `goResult` (`transfer.go:115-119`), which sends on an **unbuffered** channel: `ch := make(chan error); go func() { ch <- f() }(); return ch`. `waitOrTimeout` (`handler.go:198-209`) returns as soon as its timer fires and never reads `done` again. Default `NotifyWait` is 300ms (`config.go:46`) vs. `WebhookTimeout` 5s (`config.go:45`) — any webhook call that takes longer than 300ms (very plausible for a real network hop) leaves its goroutine permanently blocked trying to send to a channel nobody is reading, forever, until process exit. Fix: `make(chan error, 1)` so the goroutine can always deliver and exit.

## Should-fix

- **Payout reversal is a blind refund with no reconciliation.** `handler.go:166-178`: any error from `s.payouts.Send` (including retries exhausted or `ctx.Err()`) triggers `Credit(..., "payout_reversal", ...)`. If the final attempt actually reached the provider and it processed the payout but the response was lost (network blip, timeout), the account gets refunded internally while the money already left externally — a double-payout risk. Consider querying the provider by idempotency key (`ref`) before reversing when the failure is ambiguous (timeout/ctx-cancel) rather than only for network errors.
- **`PayoutClient.Send` leaks a response body when the context is canceled during backoff.** `payouts.go:81-90`: on the retryable-status path, `resp.Body.Close()` (line 89) only runs if the `select` at lines 82-86 falls through the timer case; the `case <-ctx.Done(): return PayoutResult{}, ctx.Err()` branch (line 85) returns without closing `resp.Body`, leaking the connection. Close it before that return, or restructure with a `defer`.
- **The `Location` header for a created transfer points at a route that doesn't exist.** `transfer.go:70`: `r.URL.JoinPath(...)` builds `/accounts/{id}/transfers/{transferID}`, but the only registered GET is `/transfers/{id}` (`main.go:70`). Following the advertised Location 404s. Build the location from the registered path instead, e.g. `"/transfers/" + strconv.FormatInt(t.ID, 10)`.
- **Webhook `Summary` hardcodes a /100 divisor regardless of currency exponent.** `notify.go:50`: `float64(ev.AmountCents)/100` is wrong for JPY (exponent 0) and KWD (exponent 3) — the same PR already added `FormatAmount` (`money.go:31`) to solve exactly this; use it here too: `FormatAmount(ev.AmountCents, ev.Currency)`.

## Nits

- `maxPayoutCents` (`payouts.go:14`) is one flat cents cutoff applied to every currency; its real-world value varies a lot across JPY/KWD/USD. Worth a comment if that's intentional.
- `providerPayout.AmountMinor` truncates into `int32` (`payouts.go:41,61-66`) with no bounds check inside `PayoutClient` itself — currently safe only because the sole caller enforces `maxPayoutCents`, so this is fragile if `Send` ever gets another caller.

## Verified correct

- `FormatAmount` (`money.go:31-45`) — checked by hand against all five `money_test.go:8-25` cases including negative amounts and 0/2/3-exponent currencies; correct.
- `Credit` and `Withdraw` (`store.go:144-187`) correctly propagate `tx.Commit()` errors via `return e, tx.Commit()`, unlike `CreateTransfer` (blocker 1).
- `writeStoreError`'s `ErrInsufficientFunds`/`ErrDailyLimit` branches (`handler.go:248-262`) work correctly because those sentinels are passed/returned directly rather than wrapped in `lookupError`.
- `SentSince`'s `SUM(-amount_cents)` (`store.go:129-140`) correctly nets to a positive total given `transfer_out` entries are stored with negative `AmountCents` (`store.go:225`).

## Questions

- What's the intended authorization rule for `GET /transfers/{id}`? (blocker 2)
- Should `DailyTransferLimit` be cents or whole units — the doc comment and the arithmetic disagree (blocker 5); which side is correct?
- Is blind reversal-on-any-`Send`-error acceptable, or should an ambiguous final failure query the provider before refunding?

## Noticed, out of scope

- `lookupError` (`store.go:18-27`) has no `Unwrap()` method, so `errors.Is(err, ErrNotFound)` (`handler.go:252`) is always `false` for errors from `GetAccount`/`GetTransfer`, turning every "not found" into a 500 in `requireOwner` (`auth.go:54-58`), `createTransfer`'s destination lookup (`transfer.go:37-41`), and `getTransfer` (`transfer.go:79-83`). I'm listing this here as a blocker too, not truly out of scope — flagging separately only because it's a one-line fix (`func (e *lookupError) Unwrap() error { return e.err }`) distinct from the others above.
