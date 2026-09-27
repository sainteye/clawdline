## Not ready to merge — I found 7 real bugs, several violating explicit acceptance criteria

`npm test` passing is misleading here: the test suite never exercises boundary values or the DOM layer, so it misses all of these.

**Persistence is fundamentally broken (AC 6)**
`save()` (`public/app.js:19`) is only called from the Remove handler, never from the `+`/`−` handlers. I verified live: bump a quantity, reload → it reverts to the original value. Only deletions survive a reload.

**Quantity stepper breaks its own bounds (AC 1)**
- `increment`/`decrement` in `public/cart.js:25-33` use `qty <= MAX_QTY` / `qty >= MIN_QTY` instead of strict comparisons, so they let qty go one past the edge before stopping.
- `app.js:31,33` disables buttons with `qty < MIN_QTY` / `qty > MAX_QTY` (should be `<=`/`>=`).
- Net effect, verified with a real browser: clicking `+` 10 times from qty=1 reaches **11**, and clicking `−` once from qty=1 drops to **0** (line total becomes $0.00, item just sits there, never removed). Spec says "− disabled at 1, + disabled at 10" — actual behavior disables at 0 and 11.

**Promo code doesn't track quantity changes (AC 3)**
`state.discount` is stored as a one-time computed cents value on submit, not recalculated in `render()`. Verified: apply SAVE10 → −$105.30, then increase monitor qty → discount stays frozen at −$105.30 instead of recalculating off the new subtotal (total comes out wrong: $1,846.70 instead of the correct $1,756.80).

**Error message doesn't clear on a valid code (AC 3)**
The submit handler's success branch never resets `state.promoError`. Verified: enter a bad code (error shows) → then enter `SAVE10` → the "Code not recognised" message is still on screen even though the discount applied correctly.

**Money format has no thousands separator (AC 4)**
`formatMoney` (`cart.js:13-15`) does `'$' + (cents/100).toFixed(2)` with no grouping. The default cart's subtotal renders as `$1053.00`, not the required `$1,053.00`. The unit test only checks values under $100, so it never caught this.

**Layout breakpoint is off by one pixel (AC 5)**
CSS media query is `min-width: 769px`, but the spec says two-column "at 768px and above." Screenshotted at exactly 768px — layout is still single-column/stacked.

**Horizontal scroll at 375px (AC 5)**
`.summary` has `min-width: 400px` in the base (mobile) rule, only overridden back down to 340px inside the `769px+` media query. So below that breakpoint the summary is forced to 400px wide. Measured on a real page: `scrollWidth` 420 vs `clientWidth` 375 — it does scroll horizontally, contradicting the spec.

Given the persistence and stepper-bounds issues affect the two headline features of the ticket, I'd send this back rather than merge. Want me to fix these?
