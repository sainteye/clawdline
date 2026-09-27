I reviewed the diff (Go wallet service adding transfers, payouts, currency formatting, business-day transfer limits). It's generally clean, but I found several correctness bugs worth fixing before merge, roughly in order of severity:

**1. Broken authorization on `GET /transfers/{id}`** — `main.go:70`
```go
mux.Handle("GET /transfers/{id}", owned(s.getTransfer))
```
`owned()` wraps handlers with `requireOwner` (`auth.go:47-67`), which reads `{id}` and calls `s.store.GetAccount(ctx, id)` — it assumes `{id}` is always an *account* ID. For this route `{id}` is a *transfer* ID, so the middleware ends up checking "does the caller own the account whose ID happens to equal this transfer ID?" — a completely unrelated check. Net effect:
- Legitimate parties to a transfer get 404 unless the transfer ID coincidentally equals one of their own account IDs.
- A user who *does* own an account with that numeric ID can view a transfer they have nothing to do with (amount, memo, both account IDs — leaked to an unrelated user).
`getTransfer` itself (`transfer.go:73-85`) never checks `from_account_id`/`to_account_id` against the caller. This needs a real ownership check, not reuse of `requireOwner`.

**2. No atomic balance check → overdraft race** — `store.go:250-264`
`adjustBalance` does `balance_cents = balance_cents + ?` unconditionally. Both `createTransfer` (`transfer.go:46`) and `createPayout` (`handler.go:150`) check `acct.BalanceCents < amount` against an account snapshot loaded earlier in `requireOwner`, then debit later with no floor guard. Two concurrent transfers/payouts on the same account can both pass the stale check and both debit, driving the balance negative. `ErrInsufficientFunds` is only ever raised by the handler's pre-check, never enforced by the DB update itself. Fix: make the debit conditional, e.g. `UPDATE accounts SET balance_cents = balance_cents - ? WHERE id = ? AND balance_cents >= ?` and check `RowsAffected`.

**3. Daily transfer limit is off by the currency exponent** — `config.go:25-27` vs `transfer.go:89-102`
The config doc says `DailyTransferLimit` is in "whole currency units (1000 means 1,000.00)", but `checkDailyLimit` compares it directly against `sent + amount`, which are in **minor units (cents)** from `SentSince`/`AmountCents`. With the default `DailyTransferLimit: 1000`, the effective limit becomes $10.00, not $1,000.00 — 100x too strict for a 2-decimal currency (and a different factor again for JPY/KWD). Either convert the limit to minor units using the account's currency exponent before comparing, or store/document it in minor units directly.

**4. `startOfBusinessDay` uses the UTC calendar date, not the local one** — `config.go:98-101`
```go
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```
This should be `now.In(loc).Date()`. As written, during the hours where the local date in `loc` still lags UTC's date (e.g. evenings in America/New_York), this returns a "start of day" that's actually in the *future* relative to `now`. `SentSince` then filters on `created_at >= since` with `since > now`, always returning 0 — silently disabling the daily limit for several hours every day. The new `TestStartOfBusinessDay` (`money_test.go`) doesn't catch this because none of its sample times are near local midnight; worth adding a case like 22:00/23:00 local time to actually exercise the boundary.

**5. `Location` header on transfer creation points to a route that doesn't exist** — `transfer.go:70`
```go
writeCreated(w, r.URL.JoinPath(strconv.FormatInt(t.ID, 10)).Path, t)
```
`r.URL` here is `/accounts/{id}/transfers`, so this produces `/accounts/{id}/transfers/{transferID}`. But the only registered read route is `GET /transfers/{id}` (`main.go:70`). A client following the `Location` header gets a 404. Should build the path from the transfer route, e.g. `"/transfers/" + strconv.FormatInt(t.ID, 10)`.

**6. Response body leak in payout retry loop on context cancellation** — `payouts.go:81-90`
```go
if attempt < c.maxAttempts {
	select {
	case <-time.After(retryDelay(resp, c.backoff<<(attempt-1))):
	case <-ctx.Done():
		return PayoutResult{}, ctx.Err()   // resp.Body never closed
	}
}
if resp != nil {
	resp.Body.Close()
}
```
If the context is canceled while waiting to retry, the function returns before reaching `resp.Body.Close()`, leaking the connection.

**Minor/design note:** on payout failure (`handler.go:172-179`), the code blindly credits the money back after *any* `Send` error, including a timeout/context cancellation where the provider may have actually processed the payout before the response was lost. That risks refunding the customer while the money still goes out externally. Since `Send` already carries an idempotency key, it'd be safer to query the provider's status before reversing when the failure is ambiguous (network/timeout) rather than only when it's a definite rejection (4xx).

Also worth noting: no tests were added for the new HTTP handlers (`createTransfer`, `createPayout`, `checkDailyLimit`) or the retry logic in `PayoutClient.Send` — only `FormatAmount` and `startOfBusinessDay` got tests, and the latter didn't catch bug #4.
