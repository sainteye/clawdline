## QA Verification — TICKET-88 (Editable cart page), Attempt 1

Server: `npm start` at `http://127.0.0.1:4173/`. `npm test` re-run and confirmed passing (4/4) — note the suite never exercises quantities past qty 3, subtotals ≥ $1,000, or a promo applied then followed by a quantity change, which is why it doesn't catch the bugs below.

### 1. Criterion → Result table

| # | Criterion | Result | Evidence |
|---|---|---|---|
| 1 | Quantity stepper stays 1–10, buttons disable at bounds | **FAIL** | Playwright script clicking `.inc` 9× on line 1 (default qty 1): at qty=10, `inc.disabled=false`; the 10th click moves qty to **11**, only then does the button disable. Clicking `.dec` once at qty=1 (`dec.disabled=false`) drops qty to **0**. |
| 2 | Remove last line → "Your cart is empty" + summary/Checkout fully hidden | **FAIL** | Removed all 3 lines via `.remove`. `#empty` text shown correctly ("Your cart is empty."), but `#summary` only gets an `is-hidden` class that **does not exist in cart.css** (`grep is-hidden public/cart.css` → no matches). Computed `display: block`, Checkout link `isVisible() = true`. Screenshot `/tmp/evidence/empty-state.png` shows the full Order Summary card and an active Checkout button with $0.00 totals. |
| 3 | SAVE10 (case-insens.) −10%, discount follows qty changes; unknown code shows/clears message | **FAIL** | (a) Discount frozen: applied `save10` on default cart (subtotal $1053.00) → discount −$105.30. Incremented qty on line 1 → subtotal became $1182.00 but discount **stayed at −$105.30** (expected −$118.20). (b) Stale error: applied `BOGUS` (shows "Code not recognised", correct), then applied `SAVE10` — discount applies correctly, but `#promo-error` is **still visible** with the same text (`hidden` attr `null`, `isVisible()=true`). Screenshot `/tmp/evidence/promo-stale-error.png`. |
| 4 | Money format `$1,234.50` (thousands separator) | **FAIL** | Default cart on load: `#subtotal` and `#total` both read `"$1053.00"` — no thousands separator. `formatMoney` uses `.toFixed(2)` with no grouping. Root cause visible in `public/cart.js`; reproduced live via `page.textContent('#subtotal')`. Also visible in every screenshot taken. |
| 5 | Layout: row ≥768px, stacked <768px; no horizontal scroll at 375px, Checkout fully visible | **FAIL** | Measured `getComputedStyle('.cart').flexDirection` at four widths: 1280→`row`, **768→`column`** (should be `row` per "768px and above"), 767→`column`, 375→`column`. Root cause: CSS media query is `@media (min-width: 769px)`, off by one pixel. Separately at 375px: `document.documentElement.scrollWidth=420` vs `clientWidth=375` → horizontal overflow confirmed. Cause: `.summary { min-width: 400px; ... }` in `cart.css` is **not scoped** to the desktop media query, so it forces a 400px-wide box at 375px viewport. The Checkout button's bounding box is `x:41, width:358` → right edge at x=399, past the 375px viewport (not fully visible without scrolling). Screenshots: `/tmp/evidence/layout-768.png`, `/tmp/evidence/layout-375.png` (both viewed). |
| 6 | Reload preserves items/quantities | **FAIL** | Incremented line 2 qty 1→3, then read `localStorage.getItem('cart:v1')` directly: still `null` after the qty change (confirmed twice). Reloading the page reverted qty to 1. Root cause: `app.js` only calls `save()` inside the `remove` click handler; the `inc`/`dec` handlers never call `save()`. Contrast test: a `Remove` click **does** write to localStorage correctly (`[{"sku":"KB-200",...},{"sku":"MN-27",...}]`), proving persistence plumbing works but is simply missing from the stepper handlers. |

### 2. Issues, repro steps, expected vs. actual

1. **Quantity can exceed 10 or drop to 0/negative** — `public/cart.js` `increment`/`decrement` use `<=`/`>=` instead of `<`/`>`, and `app.js`'s disabled-attribute conditions (`qty > MAX_QTY`, `qty < MIN_QTY`) are off by one.
   - Repro: on any line, click `+` until qty=10, click once more.
   - Expected: `+` disabled at 10, no further increase.
   - Actual: qty becomes 11 before the button disables (log in `/tmp/evidence/stepper.mjs` output above).
   - Same pattern in reverse for `-` at qty=1 → qty becomes 0.

2. **Empty-cart state doesn't hide the summary/Checkout** — `app.js` toggles `#summary.is-hidden`, but `cart.css` has no rule for `.is-hidden`.
   - Repro: remove all lines.
   - Expected: order summary and Checkout button hidden.
   - Actual: summary card and enabled Checkout button remain fully visible with $0.00 totals. Screenshot: `/tmp/evidence/empty-state.png`.

3. **Discount doesn't follow later quantity changes** — `app.js` computes `state.discount` as a fixed cents value at apply-time and never recomputes it when items change.
   - Repro: apply `SAVE10`, then click `+` on any line.
   - Expected: discount recalculates to 10% of the new subtotal.
   - Actual: discount stays at the original cents amount (log above: subtotal rose to $1182.00, discount frozen at −$105.30 instead of −$118.20).

4. **Stale "Code not recognised" message isn't cleared by a valid code** — `app.js`'s submit handler only ever sets `state.promoError`, never resets it to `''` on success.
   - Repro: apply an unknown code, then apply `SAVE10`.
   - Expected: error message disappears once the valid code is applied.
   - Actual: message stays visible even though the discount applied correctly. Screenshot: `/tmp/evidence/promo-stale-error.png`.

5. **No thousands separator in money values** — `formatMoney` in `cart.js` uses plain `toFixed(2)`.
   - Repro: load the page with the default cart (subtotal over $1,000).
   - Expected: `$1,053.00`.
   - Actual: `$1053.00`, everywhere an amount ≥ $1,000 is shown.

6. **768px breakpoint excludes 768px itself** — `cart.css` media query is `min-width: 769px`.
   - Repro: set viewport to exactly 768px wide.
   - Expected: two-column (row) layout per "768 px and above".
   - Actual: stacked (column) layout; measured `flexDirection: column`. Screenshot: `/tmp/evidence/layout-768.png`.

7. **Horizontal scroll at 375px, Checkout button not fully visible** — unscoped `.summary { min-width: 400px }` rule applies below the desktop breakpoint too.
   - Repro: set viewport to 375px wide, load page.
   - Expected: no horizontal scroll, Checkout fully on-screen.
   - Actual: `scrollWidth 420 > clientWidth 375`; Checkout button's right edge at x=399 (24px past the viewport). Screenshot: `/tmp/evidence/layout-375.png`.

8. **Quantity changes are never persisted** — `app.js`'s `.inc`/`.dec` click handlers don't call `save()` (only `.remove` does).
   - Repro: increase any line's quantity, reload the page.
   - Expected: quantity retained.
   - Actual: `localStorage['cart:v1']` stays `null` after a qty change; reload reverts to the default quantity. Removal, by contrast, does persist correctly (confirmed).

### 3. Verdict: **FAIL — needs work**

All six acceptance criteria have at least one reproducible, live-verified failure (boundary steppers, empty-state hiding, promo discount tracking and error clearing, money formatting, the 768px breakpoint plus 375px horizontal overflow, and — most significantly — that quantity changes aren't persisted at all). `npm test` passing and a manual Chrome check are not sufficient evidence here: the existing unit tests don't cover amounts over $1,000, qty boundaries, or promo-then-qty-change sequences, which is exactly where the bugs live. Not ready to merge.
