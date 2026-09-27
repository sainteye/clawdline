## QA Verification — TICKET-88 (Attempt 1)

### Results table

| # | Criterion | Result | Evidence |
|---|---|---|---|
| 1 | Quantity stepper clamps 1–10, buttons disable at bounds | **FAIL** | Scripted click-through of `+`/`−` on line 1: qty went **1→11** before `+` disabled (disabled only at 11, not 10); qty went **11→0** before `−` disabled (disabled only at 0, not 1). |
| 2 | Remove / empty state hides whole summary incl. Checkout | **FAIL** | Removed all 3 lines: "Your cart is empty." appeared correctly, but `#summary` and `.checkout` stayed visible (`isVisible() === true` for both). Screenshot: order summary + Checkout button still shown under the empty message. |
| 3 | Promo code: SAVE10 case-insensitive, discount follows qty changes, error clears on valid code | **FAIL** | Applied `FREESHIP` → error shown (correct). Applied `save10` after → discount applied correctly but error text **stayed visible**. Then incremented a line qty: subtotal moved $1053.00→$1182.00 but discount **stayed frozen at −$105.30** instead of recalculating to −$118.20. |
| 4 | Money format with thousands separator, e.g. `$1,234.50` | **FAIL** | Subtotal/total rendered as `$1053.00`, not `$1,053.00` — `formatMoney` never inserts a thousands separator. |
| 5 | Layout: side-by-side ≥768px, stacked <768px; no horizontal scroll at 375px, Checkout fully visible | **FAIL** | At exactly 768px width, measured layout is still stacked (summary not right of items) — CSS breakpoint is `min-width:769px`, off by one. At 375px, `document.documentElement.scrollWidth` (420px) > `clientWidth` (375px): page scrolls horizontally and cuts off "Remove", totals, and the Checkout button (screenshots attached). |
| 6 | Persistence across reload | **FAIL (partial)** | Removing a line persists after reload (confirmed). Changing quantity does **not**: set qty to 3, reloaded, qty reverted to 1 — `save()` is never called from the +/− handlers in `app.js`. |

Unit tests (`npm test`) do pass, but they don't cover any of these edge cases (boundary quantities, thousands separator, discount-after-qty-change, error-clearing, or persistence via the stepper), which is why they didn't catch the regressions.

### Issues found

**1. Quantity not clamped to 1–10** (`public/cart.js:26,31`, `public/app.js:33,31`)
- Repro: click `+` on any line 10 times from qty 1.
- Expected: qty stops at 10, `+` disabled.
- Actual: qty reaches 11, `+` only disables at 11. Symmetric bug on `−` allows qty to reach 0.
- Cause: `increment`/`decrement` use `<= MAX_QTY` / `>= MIN_QTY` (should be `<`/`>`); the disabled attributes in `app.js` use `qty > MAX_QTY` / `qty < MIN_QTY` (should be `>=`/`<=`).

**2. Empty cart doesn't hide the order summary**
- Repro: remove all lines.
- Expected: summary and Checkout button hidden.
- Actual: both remain fully visible (screenshot `empty-state.png`).
- Cause: `render()` toggles a CSS class `is-hidden` on `#summary` (`public/app.js:52`) that is never defined in `public/cart.css`.

**3a. "Code not recognised" doesn't clear after a valid code**
- Repro: apply `FREESHIP`, then apply `SAVE10`.
- Expected: error message disappears.
- Actual: error text remains visible alongside the applied discount.
- Cause: the submit handler (`public/app.js:63-72`) only sets `state.promoError` on failure, never clears it on success.

**3b. Discount doesn't follow later quantity changes**
- Repro: apply `SAVE10`, then increment a line's quantity.
- Expected: total discount recalculates as 10% of the new subtotal.
- Actual: discount cents amount stays frozen at the value computed at apply-time.
- Cause: `state.discount` stores an absolute cents amount instead of the rate; `render()` never recomputes it.

**3c. (related, cosmetic) "Promo (SAVE10) −$0.00" row shows even with no discount applied**
- The `hidden` attribute is correctly set on `#discount-row`, but `.totals div { display:flex }` in `cart.css` outranks the UA `[hidden]{display:none}` rule, so the row stays visible with `−$0.00` on every fresh page load.

**4. No thousands separator in money format**
- Repro: view subtotal/total on the default cart ($1,053 pre-tax).
- Expected: `$1,053.00`.
- Actual: `$1053.00`. Cause: `formatMoney` (`public/cart.js:13-15`) is `'$' + (cents/100).toFixed(2)` with no grouping.

**5a. 768px breakpoint stacked instead of side-by-side**
- Measured bounding boxes at 768px width: summary is not right of items list.
- Cause: `@media (min-width: 769px)` in `cart.css:47` should be `768px`.

**5b. Horizontal scroll and cut-off Checkout at 375px**
- `scrollWidth` 420px vs `clientWidth` 375px at a 375px viewport; "Remove", totals, and Checkout are clipped off-screen (screenshots `mobile-375-viewport.png`, `mobile-375-scrolled-right.png`).
- Cause: `.summary { min-width: 400px; }` (`cart.css:32`) applies unconditionally and is only overridden inside the `min-width:769px` media query, so mobile widths still force a 400px-wide summary.

**6. Quantity changes are not persisted**
- Repro: change a line's quantity, reload the page.
- Expected: quantity unchanged after reload.
- Actual: reverts to the value from `localStorage` (or default) since the last removal.
- Cause: `save()` (`public/app.js:19-21`) is called from the `.remove` handler but never from the `.inc`/`.dec` handlers.

### Verdict: **FAIL — needs work**

All six acceptance criteria have at least one confirmed, reproducible failure in the running app (not just code-reading): out-of-range quantities, a summary that never hides, a promo discount that both fails to clear its error and fails to track quantity changes, missing thousands separators, a broken 768px breakpoint plus horizontal scroll/clipping at 375px, and quantity changes that don't survive a reload. The `npm test` pass and "looks good in Chrome" from the developer's summary did not catch any of these because the existing tests don't exercise boundary values, and manual spot-checking apparently missed the empty-state, promo, and mobile layout paths. This needs another development pass before merge.
