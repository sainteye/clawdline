I ran a review of `pr.diff` (a payments/banking-style Go service — accounts, transfers, payouts, notifications). It found 15 confirmed issues, several of which are serious correctness/money-safety bugs. Here they are, most severe first:

**Critical (money/security correctness)**

1. **`auth.go:54` — `requireOwner` authorizes the wrong entity for transfers.** The `GET /transfers/{id}` route reuses account-ownership middleware, but `{id}` there is a transfer ID, not an account ID. Since account and transfer IDs are independent counters, a user can view another user's transfer just by owning an account with the same numeric ID — cross-tenant data leak.

2. **`store.go:185` — shadowed `err` breaks transaction rollback in `CreateTransfer`.** The guard clauses at lines 205/208/217 use `if err := ...; err != nil` which shadows the outer `err` the deferred commit/rollback closure reads. If the second `adjustBalance` (crediting the recipient) fails after the debit succeeded, the deferred logic still sees a nil outer `err` and **commits** instead of rolling back — money is debited, never credited, and the client is told the transfer failed.

3. **`payouts.go:80` — shadowed `res`/`err` in the retry loop returns a fake success.** After all retries in `Send` fail, the function falls through to `return res, err` referring to *outer* variables that were never touched by the loop's inner `res, err :=`. This returns `(PayoutResult{}, nil)` — i.e., "success" — even though every attempt failed, so the caller thinks the payout was accepted when it wasn't.

**High severity**

4. **`money.go:39` — `FormatAmount` mangles negative amounts.** Go truncating division/modulo makes both quotient and remainder negative for non-whole negative values, producing strings like `"-5.-30"` instead of `"-5.30"`. Debit ledger entries are stored as negative cents per the code's own convention, so this corrupts real user-facing output and isn't caught because tests only use positive values.

5. **`store.go:80` — `%v` instead of `%w` breaks `errors.Is(ErrNotFound)`.** This turns intended 404s into 500s, and also creates an oracle (404 vs 500) that defeats the anti-enumeration comment right above `requireOwner`.

6. **`payouts.go:106` — response body leak on 429/5xx.** `defer resp.Body.Close()` is registered after the early-return path for retryable status codes, so every retry leaks a connection.

7. **`notify.go:38` — goroutine leak in `NotifyWithin`.** Unbuffered `done` channel + fire-and-forget goroutine means any notification slower than `NotifyWait` (300ms default, common) leaks the goroutine forever once the timeout path abandons it.

8. **`config.go:99` — `startOfBusinessDay` uses UTC date instead of local date**, silently disabling the daily transfer limit for several hours every evening in non-UTC zones (e.g. America/New_York). Existing tests happen to avoid the boundary and don't catch it.

**Medium**

9. **`handler.go:239` — `writeCreated` sets the `Location` header after `writeJSON` already wrote the status/body**, so the header is silently dropped.

10. **`transfer.go:89` — daily-limit check and balance update aren't atomic**, allowing concurrent requests to race past the daily limit and drive balance negative.

11. **`handler.go:172` — payout reversal on any `Send` error, including ambiguous ones** (timeout/dropped response) where the provider may have already executed the payout — risks double-crediting the customer while the external payout still lands.

12. **`handler.go:174` — reversal uses `r.Context()`**, so if the original failure was a client-cancelled context, the compensating credit is doomed to fail immediately too (logged as "manual fix needed").

13. **`main.go:50` — server `WriteTimeout` (15s) shorter than payout's worst-case retry duration (~30.75s)**, so a slow-but-successful provider call can have its response torn away mid-flight.

14. **`handler.go:155` — no idempotency key for payouts/transfers/deposits**, so client-side retries after a timeout can cause real duplicate debits/payouts.

15. **`transfer.go:67` — two sequential `notify` calls** for transfer_out/transfer_in add unnecessary latency (up to ~2×`NotifyWait`) instead of running concurrently.

The top three (#1–#3) look like they'd need fixing before this ships — they're authorization and money-correctness bugs, not edge cases. Want me to draft fixes for any of these?
