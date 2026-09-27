I read through `pr.diff` (a Go wallet service adding transfers, payouts, currency formatting, and a daily transfer limit). It's well-organized, but I found several concrete bugs — two of them serious enough to block merge.

## Critical

**1. Broken authorization on `GET /transfers/{id}` — cross-account data exposure** (`main.go:72`, `auth.go:47-64`, `transfer.go:73-85`)

```go
mux.Handle("GET /transfers/{id}", owned(s.getTransfer))
```
`owned()` wraps every route in `requireOwner`, which treats the `{id}` path value as an **account** ID and checks that the caller owns that account (`auth.go:54`). But for this route, `{id}` is a **transfer** ID. `getTransfer` (`transfer.go:73-85`) never checks that the transfer actually belongs to the caller — it just loads and returns whatever transfer matches the URL.

Failure scenario: a user who owns account `#7` calls `GET /transfers/7`. `requireOwner` loads account `#7` (theirs) and passes the check. `getTransfer` then returns transfer `#7` — completely unrelated to that user, potentially between two other customers. Since account IDs and transfer IDs are independent auto-increment sequences, any user can view other customers' transfers (amounts, memos, counterparty account IDs) just by owning an account whose ID number happens to match a transfer ID. This is a real IDOR, not a theoretical one.

**2. `PayoutClient.Send` reports success after all retries fail — silent fund loss** (`payouts.go:72`, `payouts.go:92`)

```go
for attempt := 1; attempt <= c.maxAttempts; attempt++ {
    resp, err := c.post(ctx, body, p.Reference)   // <-- shadows outer err
    ...
}
return PayoutResult{}, err   // <-- refers to the OUTER err, which is nil
```
`resp, err := c.post(...)` inside the loop declares a **new**, block-scoped `err` that shadows the function-level `err` from the earlier `json.Marshal` call. When every retry attempt fails (network errors or persistent 429/5xx), the loop exits normally and the final `return PayoutResult{}, err` returns the outer `err` — which is still `nil`.

Effect in `handler.go`'s `createPayout`: the account has already been debited via `Withdraw`, the payout never succeeded, but `Send` returns `(PayoutResult{}, nil)`, so the error branch (which reverses the debit) is skipped. The handler responds `202 Accepted` with an empty `provider_id`/`status`, telling the customer their payout succeeded while their money is simply gone. This is the kind of bug that's easy to miss because it only shows up when the provider is down long enough to exhaust retries — exactly the scenario retries exist for. `go vet`'s default checks won't catch this (shadow analysis isn't part of the default vet suite).

## High

**3. `startOfBusinessDay` computes the wrong calendar day, which can bypass the daily limit entirely** (`config.go:98`)

```go
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
    y, m, d := now.UTC().Date()
    return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
```
This takes the calendar date from `now.UTC()`, not from `now` as seen in `loc`. For negative UTC-offset zones (e.g. `America/New_York`), during roughly 7–8pm–midnight local time, the UTC date has already rolled to tomorrow, so this returns **midnight of tomorrow** in `loc` — a timestamp in the future. `checkDailyLimit` (`transfer.go:93-99`) then calls `SentSince(ctx, accountID, since)` with that future `since`, which will always find zero prior transfers (`created_at >= since` matches nothing), so **the daily limit silently stops being enforced for several hours every evening**.

The new test (`money_test.go: TestStartOfBusinessDay`) doesn't catch this — every test case's hour/offset combination happens to avoid crossing a UTC date boundary (e.g. New York at 09:30/12:00, Tokyo at 17:45), so it passes despite the bug. Fix: use `now.In(loc).Date()`. Worth adding a test case near local midnight for a zone with a large offset to prove it.

**4. `DailyTransferLimit` unit mismatch — enforced limit is 100x smaller than documented** (`config.go:25-28`, `config.go:47`, `transfer.go:98`)

```go
// DailyTransferLimit is the most one account may send to other accounts
// per business day, in whole currency units (1000 means 1,000.00).
DailyTransferLimit int64
...
if sent+amount > s.opts.DailyLimit {
```
The doc says `1000` means `$1,000.00` (major units), but `sent` and `amount` are both in minor units/cents (`SentSince` sums `amount_cents`; `amount` is `req.AmountCents`). Comparing them directly means the default `DailyTransferLimit: 1000` actually caps daily transfers at **1000 cents ($10.00)**, not $1,000. Either the comment is wrong or the comparison needs `* 100` (currency-exponent-aware, ideally reusing the `currencyInfo` table in `money.go`) — as written, operators setting `WALLET_DAILY_TRANSFER_LIMIT` will get a wildly different limit than they expect.

## Medium

**5. Webhook `Summary` ignores currency exponent** (`notify.go:50`)
```go
ev.Summary = fmt.Sprintf("%s of %.2f %s on account %d", ev.Type, float64(ev.AmountCents)/100, ev.Currency, ev.AccountID)
```
Hardcodes `/100`, but `money.go`'s new `currencyInfo` table explicitly supports JPY (0 decimals) and KWD (3 decimals). A 500 JPY entry would show "5.00 JPY" instead of "500 JPY"; a 1250 KWD entry would show "12.50 KWD" instead of "1.250 KWD". Should call `FormatAmount(ev.AmountCents, ev.Currency)` instead.

**6. `Location` header from `createTransfer` points to a route that doesn't exist** (`transfer.go:70`)
```go
writeCreated(w, r.URL.JoinPath(strconv.FormatInt(t.ID, 10)).Path, t)
```
The request URL is `/accounts/{id}/transfers`, so this produces `Location: /accounts/{id}/transfers/{transferID}` — but the only registered retrieval route is `GET /transfers/{id}` (`main.go:72`). A client following the `Location` header gets a 404.

**7. Auto-reversal on payout failure risks a phantom credit** (`handler.go:280-286`)
If `PayoutClient.Send` fails after the provider actually processed the request (e.g. the response was lost to a network error on the last attempt), the handler credits the money back to the wallet while the external payout still goes through — double payout from the business's perspective. Worth considering whether reversal should wait for a provider-side status check/webhook rather than reversing on any `Send` error.

## Low

- **`payouts.go:84-90`**: if `ctx.Done()` fires while waiting between retries, the function returns immediately without closing the previous response's body — a connection/resource leak on cancellation.
- **`transfer.go:46,50`**: the balance-sufficiency check and the daily-limit check both read-then-act with no locking/transaction isolation, so concurrent transfers from the same account can race past both checks and overdraw the account or exceed the daily limit.

---

Items 1 and 2 are the ones I'd block merge on — one's a cross-tenant data leak, the other silently loses customer funds while reporting success. Want me to draft fixes for any of these?
