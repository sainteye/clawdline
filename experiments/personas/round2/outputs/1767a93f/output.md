I read through `pr.diff` and cross-checked every changed function against its callers (auth.go, config.go, handler.go, main.go, money.go, notify.go, payouts.go, store.go, transfer.go). This is a wallet service, so I traced the money-movement paths closely. I found several serious, independently-verified bugs — most caused by classic Go `:=` scoping gotchas that are easy to miss in review.

## Verdict: needs rework

There are multiple blockers touching fund correctness and access control. None require guessing — each is traceable in the diff.

## Blockers

**1. `Store.CreateTransfer` can commit a partially-applied transfer (store.go:191-233)**
The deferred commit/rollback closure checks the function-scoped `err` set by `BeginTx`/`ExecContext`/`LastInsertId`. But the two balance updates and both ledger-entry inserts use `if err := adjustBalance(...); err != nil { return ... }` and `if _, err := insertEntry(...); err != nil { return ... }` (store.go:216, 219, 228) — each `:=` inside the `if` declares a *new*, block-scoped `err` that shadows the outer one. If any of those four calls fails after the transfer row insert succeeds, the handler returns an error to the caller, but the outer `err` the defer checks is still `nil`, so `defer` calls `tx.Commit()` anyway. Concretely: if the from-account debit succeeds but the to-account credit fails (e.g. a transient DB error, or the destination account is deleted concurrently), the debit is committed, the credit and both ledger entries are not, and the API still reports failure — silent money loss with no ledger trail. `Credit`/`Withdraw` don't have this bug; they use unconditional `defer tx.Rollback()` with no conditional commit, so this is specific to the new `CreateTransfer`.

**2. `GET /transfers/{id}` has broken/wrong authorization (main.go:70, auth.go:47-67, transfer.go:73-85)**
`mux.Handle("GET /transfers/{id}", owned(s.getTransfer))` wraps `getTransfer` with the same `requireOwner` used for `/accounts/{id}/...`. `requireOwner` treats `{id}` as an *account* ID (auth.go:54, `s.store.GetAccount(ctx, id)`) and checks that account's `OwnerID`. Here `{id}` is a *transfer* ID. Since account IDs and transfer IDs are independent auto-increment sequences that both start at 1, low IDs overlap routinely. Net effect: a user can view transfer #N merely by owning account #N — regardless of whether they're the sender or recipient — and `getTransfer` itself never checks `t.FromAccountID`/`t.ToAccountID` against the caller. Meanwhile the *legitimate* case (checking your own transfer) will 404 unless you happen to own an account with that same ID. This is both an IDOR and a functionally broken endpoint.

**3. `startOfBusinessDay` uses the wrong calendar date (config.go:98-99)**
```go
y, m, d := now.UTC().Date()
return time.Date(y, m, d, 0, 0, 0, 0, loc)
```
This takes the *UTC* calendar date, not the date in `loc`. For any zone whose offset isn't zero, there's a daily window (equal to the offset) where these differ. Example: `startOfBusinessDay(2026-03-14 02:00 Asia/Tokyo, Asia/Tokyo)` — 2am JST is 17:00 UTC on **March 13**, so this returns `2026-03-13 00:00 JST` instead of the correct `2026-03-14 00:00 JST`, a full day off. For `America/New_York` the error window falls in the evening instead. `money_test.go`'s `TestStartOfBusinessDay` only exercises hours (9:30, 12:00, 17:45, 18:00, 23:59) that happen to avoid this boundary in every listed zone, so it passes despite the bug. Fix: `now.In(loc).Date()`.

**4. `PayoutClient.Send` returns a nil error after exhausting retries (payouts.go:71-92)**
```go
for attempt := 1; attempt <= c.maxAttempts; attempt++ {
    resp, err := c.post(ctx, body, p.Reference)   // shadows outer err
    ...
}
return PayoutResult{}, err   // refers to the OUTER err (nil, from json.Marshal)
```
`resp, err := c.post(...)` is a new declaration scoped to the loop body (since `resp` is new), shadowing the `err` from `body, err := json.Marshal(...)` before the loop. When all attempts fail (persistent 5xx/429 or network errors), the function falls through to `return PayoutResult{}, err`, where `err` is the *outer*, still-nil variable. `createPayout` (handler.go:166-186) sees `err == nil`, skips the reversal, and responds `202 Accepted` with an empty `provider_id`/`status` — even though the account was already debited (`s.store.Withdraw`, handler.go:160) and the provider never accepted the payout.

**5. Daily transfer limit is off by 100x for normal currencies (transfer.go:89-102, config.go:25-28)**
`Config.DailyTransferLimit` is documented as "whole currency units (1000 means 1,000.00)", but `checkDailyLimit` compares it directly against cent sums: `sent` (from `SentSince`, which sums `amount_cents`) plus `amount` (`req.AmountCents`) against `s.opts.DailyLimit` with no ×100 conversion. The default `DailyTransferLimit: 1000` (config.go:47), intended as a $1,000/day cap, is actually enforced as $10.00/day. No test exercises `checkDailyLimit`, so this shipped uncaught.

**6. No overdraft guard on concurrent debits (store.go:249-264, transfer.go:46, handler.go:150)**
`adjustBalance` does an unconditional `UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ?` with no `balance_cents >= ...` guard, and the only sufficient-funds checks (`from.BalanceCents < req.AmountCents` in `createTransfer`/`createPayout`) are against an account snapshot loaded once in `requireOwner`, not re-checked inside the debiting transaction. Two concurrent transfers/payouts from the same account each read the same stale balance, both pass their own check, and both debit — there's nothing in the SQL or the transaction to stop the balance from going negative.

**7. Payout reversal reuses a context that may already be canceled (handler.go:166-176)**
`s.payouts.Send(r.Context(), ...)` and the compensating `s.store.Credit(r.Context(), ...)` both use the inbound request's context. If the client disconnects while `Send` is retrying, `r.Context()` cancels, `Send` returns early with `ctx.Err()` (payouts.go:85), and the reversal `Credit` call — using the same canceled context — fails immediately too (`BeginTx` rejects a done context), leaving the account debited with neither a completed payout nor a reversal, just a "manual fix needed" log line. `NotifyWithin` (notify.go:36-43) explicitly avoids this by using `context.Background()` for exactly this reason; the reversal path doesn't get the same treatment.

## Should-fix

- **Wrong `Location` header on transfer creation** (transfer.go:70): `writeCreated(w, r.URL.JoinPath(strconv.FormatInt(t.ID, 10)).Path, t)` builds `/accounts/{id}/transfers/{transferID}`, but the only route that serves a transfer is `GET /transfers/{id}` (main.go:70). The header points at a path with no matching route.
- **Response body leak on cancellation** (payouts.go:81-90): when `ctx.Done()` fires during the backoff `select`, the function returns immediately; the `resp.Body.Close()` a few lines later is never reached for that attempt.
- **No client-supplied idempotency key** on `POST .../transfers` or `.../payouts`: the reference is generated server-side per call (transfer.go:104-111), so a client-side retry after a timeout (common on flaky networks) creates a second real transfer/payout rather than being deduplicated. The PR added provider-side retry dedup but nothing to protect against the caller retrying the whole request.
- **No tests for the new financial logic**: `checkDailyLimit`'s unit handling, `CreateTransfer`'s atomicity, `PayoutClient.Send`'s retry/error paths, and `requireOwner`+`getTransfer` are exactly where blockers 1–6 live, and none are covered by `money_test.go`.

## Nits

- `providerPayout.AmountMinor` is `int32` while everything else uses `int64` (payouts.go:41); currently safe since `maxPayoutCents` (25,000,000) fits, but there's no assertion tying the two together if the limit is raised later.
- Even after fixing #5, a single global `DailyTransferLimit` integer can't represent an equivalent cap across currencies with different `Exponent` (JPY=0 vs KWD=3) — worth a comment or per-currency handling if that matters.

## Verified correct

- `FormatAmount` (money.go): matches all cases in `TestFormatAmount`, including sign handling and the JPY/KWD exponent differences.
- All store.go queries use parameterized placeholders — no injection risk.
- `requireOwner`'s "not found" for both a missing account and a mismatched owner (auth.go:59-63) correctly avoids leaking account existence.
- `decodeJSON` caps body size at 64KB and rejects unknown fields.

## Questions

- Is `GET /transfers/{id}` meant to be visible to both parties of the transfer? If so it needs its own authorization check, not `requireOwner`.
- Is excluding payouts from `DailyTransferLimit` (only `transfer_out` is summed) intentional, per "send to other accounts" in the config comment?
