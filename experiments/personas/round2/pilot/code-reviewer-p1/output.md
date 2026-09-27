I dug into the diff directly (the automated review pipeline fragmented mid-run, so I verified everything against the actual code rather than relying on partial notifications). This PR adds transfers, payouts, multi-currency formatting, and a daily transfer limit to a Go wallet service. It's a big diff and there are several real, serious bugs — mostly around transaction-error handling and money movement. Here's what I found, worst first.

## Critical

1. **IDOR on `GET /transfers/{id}`** — `main.go` wraps `getTransfer` with `requireOwner`, but `requireOwner` (`auth.go:47-67`) treats the path `{id}` as an *account* id and checks that you own that account. `getTransfer` (`transfer.go:74-86`) then reuses the same `{id}` as a *transfer* id and does zero check that you're a party to it. Result: if the id in the URL happens to match one of your own account ids, you get back an unrelated transfer record — other people's transfers, exposed.

2. **`%v` instead of `%w` breaks 404s everywhere** — `store.go:80`: `GetAccount` wraps `ErrNotFound` with `%v`, so `errors.Is(err, ErrNotFound)` in `writeStoreError` never matches. Every route that loads an account (accounts, entries, deposits, transfers, payouts, and the destination lookup in `createTransfer`) now 500s instead of 404s on a missing account.

3. **Shadowed `err` commits broken transfers** — `store.go:190-232`. The deferred commit/rollback at `store.go:195-201` reads the function-scope `err`, but lines 215, 218, and 227 all declare a *new*, shadowed `err` via `if err := ...; err != nil`. If `adjustBalance` or `insertEntry` fails after the transfer row is inserted, `CreateTransfer` returns an error to its caller — but the outer `err` is still `nil`, so the defer calls `Commit()` anyway. A caller sees an error while a half-applied transfer (wrong balances and/or missing ledger entries) gets permanently persisted.

4. **Same shadowing bug turns a fully-failed payout into a fake success** — `payouts.go:80`: `res, err := c.post(...)` inside the retry loop shadows the outer `res`/`err`. If every attempt fails with a retryable error, `Send` returns the untouched outer values: `PayoutResult{}, nil`. `createPayout` treats `err == nil` as success and responds 202 with an empty `provider_id`/`status` — after already debiting the account, with no refund ever triggered.

5. **Daily transfer limit is off by ~100x** — `config.go` documents `DailyTransferLimit` in *whole currency units* (default `1000` = $1,000.00), but `checkDailyLimit` (`transfer.go:90-103`) compares it directly against amounts in cents. The default limit is actually enforced as 1000 **cents** = $10/day.

## Medium

6. **Payout refund uses the request context** (`handler.go:170`) — if `Send` failed because the client disconnected or the request deadline hit, the compensating `Credit` inherits that same cancelled context and can fail too, leaving the account debited with nothing but a "manual fix needed" log line.
7. **Refund fires on any error, not just a definite rejection** (`handler.go:168-174`) — a timeout doesn't mean the provider didn't pay out. Auto-refunding on ambiguous errors risks a double-spend.
8. **`WriteTimeout` (15s) < worst-case payout retry budget** (~30.75s: 3×10s + backoff) — the server can cut the response mid-retry; the client retries with a fresh `newReference` (a new idempotency key), risking a duplicate payout at the provider.
9. **Leaked response body on retryable errors** — `payouts.go:106-109`: the 429/5xx early return happens before `defer resp.Body.Close()` is registered, leaking the connection on exactly the path that repeats under retries.
10. **Goroutine leak on webhook timeout** — `notify.go:41-51`: `done` is unbuffered, so when `NotifyWithin` times out, the spawned goroutine blocks forever trying to send its result to nobody. Every slow-webhook notification leaks a goroutine. Fix: `make(chan error, 1)`.
11. **TOCTOU on transfer balance** — `transfer.go:46-48` checks a pre-transaction balance snapshot; `adjustBalance` never re-checks the floor atomically (unlike `Withdraw`'s `WHERE balance_cents >= ?`). Concurrent transfers can overdraw an account. Same race applies to the daily-limit check.
12. **`startOfBusinessDay` picks the wrong day for non-UTC zones** — `transfer.go:107-110` takes the calendar date from `now.UTC()` and reinterprets it in `loc` (default `America/New_York`), which is wrong near UTC midnight and can even put `since` in the future, silently zeroing out "sent today." The included test only checks 15:09 local time, so it doesn't catch this.

## Minor

13. `Withdraw` maps "account not found" and "insufficient funds" to the same `ErrInsufficientFunds` (409 instead of 404) — `store.go:170-173`.
14. `WALLET_NOTIFY_WAIT`/`WALLET_PAYOUT_TIMEOUT` aren't validated as positive, unlike `WALLET_MAX_PAGE_SIZE` — a `0` value silently produces degenerate behavior.
15. `w.Header().Set("Location", ...)` in `transfer.go:71` runs *after* `writeJSON` already wrote the response headers — it's dropped on every `201`. Swap the order.

Items 3, 4, and 5 are the ones I'd block the PR on — they're not edge cases, they're wrong on the happy-adjacent path. Want me to fix any of these?
