## QA Verification — TICKET-88 (Editable cart page), Attempt 1

**Verdict: FAIL — needs work.** 7 of the 8 checklist items fail. `npm test` passes only because the existing unit tests don't cover any boundary case (qty=1/qty=10, thousands-separator amounts, discount-follows-quantity, or persistence of quantity changes).

### Criteria table

| # | Criterion | Result | Evidence |
|---|---|---|---|
| 1 | Quantity stepper (bounds 1–10, buttons disable correctly) | **FAIL** | `scripts/qa-interact.mjs`: clicking `+` 9× from qty=1 reaches 10, but `inc.isDisabled()` is `false` at qty=10; one more click pushes qty to **11**. Clicking `−` from qty=1 similarly pushes qty to **0** before `dec` disables. |
| 2 | Remove and empty state (hides summary + Checkout) | **FAIL** | `/tmp/qa-shots/empty-cart.png` — after removing all lines, "Your cart is empty." shows correctly, but the Order summary panel and **Checkout button remain fully visible**. |
| 3 | Promo code (case-insensitive, error clears, discount follows qty) | **FAIL** | Script output: applying `save10` (lowercase) after a bad code leaves `#promo-error` still visible (`Valid code error hidden now? false`). After qty+1 post-promo, subtotal became `$1182.00` but discount stayed `−$105.30` (should recompute to `$118.20`) — discount does not follow quantity changes. |
| 4 | Money format ($1,234.50 style, thousands separator) | **FAIL** | `desktop-1280.png` / `tablet-768.png`: default cart subtotal shown as `$1053.00`, not `$1,053.00`. `formatMoney` (`public/cart.js:13-15`) uses `.toFixed(2)` with no separator. |
| 5 | Layout (≥768px two-column, 375px no h-scroll, Checkout fully visible) | **FAIL** | Screenshot at 768px viewport is **420×800** actually — wait, at width=768 the page rendered stacked, not two-column (`/tmp/qa-shots/tablet-768.png`); script confirms `summary.x=20, y=321` (below, not beside, items). CSS media query is `@media (min-width: 769px)` — off by one. At 375px viewport, full-page screenshot is **420px wide** (`file` command), `scrollWidth − clientWidth = 45px` overflow, and the Checkout button bounding box (`x:41,width:358`) extends to x=399, past the 375px viewport. |
| 6 | Persistence (reload keeps items/qty) | **FAIL** | Script: incremented qty 1→3, reloaded, qty reverted to **1**. `app.js` only calls `save()` in the `.remove` handler, never in `.inc`/`.dec` handlers, so quantity edits are never persisted. |
| — | `npm test` | PASS (as claimed) | `npm test` → 4/4 pass, but tests don't exercise any of the above boundary/format/persistence cases, so they don't catch the bugs. |
| — | Chrome-visible check by developer | N/A | Not independently reproducible, but contradicted by the above — the default cart's subtotal is wrong ($1053.00) and the 768px layout is stacked, both visible on first load in any browser. |

### Issues found

1. **Qty stepper exceeds bounds and buttons don't disable at the right edge.**
   Repro: open cart, click `+` on any line 9 times (qty 1→10) — `+` button is still enabled; click once more — qty becomes 11. Same for `−`: from qty=1, click once more — qty becomes 0.
   Expected: `+` disabled once qty=10 (never reaches 11); `−` disabled once qty=1 (never reaches 0).
   Actual: both go one step past the limit before disabling.
   Root cause: `public/cart.js` `increment`/`decrement` use `<= MAX_QTY` / `>= MIN_QTY` (off by one), and `public/app.js` disabled conditions (`item.qty < MIN_QTY`, `item.qty > MAX_QTY`) never trigger at the actual boundary.

2. **Empty cart still shows the order summary and Checkout button.**
   Repro: remove all lines. See `/tmp/qa-shots/empty-cart.png`.
   Expected: summary/Checkout hidden.
   Actual: `#summary` gets `is-hidden` class toggled in `app.js`, but `.is-hidden` is never defined in `public/cart.css` — no visual effect.

3. **Promo error message doesn't clear on a valid code; discount freezes and doesn't follow quantity changes.**
   Repro: submit an unknown code (error shows), then submit `save10` — error text stays visible. Then apply `save10` on the default cart (discount `−$105.30`), increment a line's qty — subtotal updates to `$1182.00` but discount stays `−$105.30` instead of recalculating to `$118.20`.
   Expected per AC3: error clears on a valid code; discount "follows later quantity changes."
   Root cause: `app.js` promo submit handler never resets `state.promoError = ''` in the success branch, and `render()` never recomputes `state.discount` from the current subtotal/rate — it's a one-time cents snapshot.

4. **Money isn't formatted with a thousands separator.**
   Repro: load the page with the default cart (subtotal > $1,000). See `desktop-1280.png` / `tablet-768.png`: shows `$1053.00`.
   Expected: `$1,053.00` per AC4.
   Root cause: `formatMoney` in `public/cart.js` is `'$' + (cents/100).toFixed(2)`, no separator logic.

5. **768px viewport shows the stacked (mobile) layout, not two-column.**
   Repro: `node scripts/shot.mjs http://127.0.0.1:4173/ 768 out.png`, or set viewport to 768×900 — summary renders below the item list.
   Expected per AC5: "At widths of 768 px and above, the summary sits to the right."
   Root cause: `public/cart.css` media query is `@media (min-width: 769px)`, excluding exactly 768px.

6. **Horizontal overflow at 375px; Checkout button not fully visible.**
   Repro: `node scripts/shot.mjs http://127.0.0.1:4173/ 375 out.png` → output PNG is 420×800, not 375-wide. `document.documentElement.scrollWidth - clientWidth = 45`.
   Expected per AC5: nothing scrolls horizontally at 375px, Checkout fully visible.
   Actual: page requires horizontal scroll; Checkout button's right edge (x≈399) is off-screen.
   Root cause: `.summary { min-width: 400px; }` in `cart.css` applies unconditionally (not just inside the `≥769px` media query), forcing 400px+ width even when stacked on narrow viewports.

7. **Quantity changes are not persisted; only removals are.**
   Repro: increment a line's qty (e.g., 1→3), reload the page — qty reverts to 1 (or whatever was last saved by a remove action).
   Expected per AC6: reload shows the same items and quantities.
   Root cause: `public/app.js` calls `save()` only inside the `.remove` click handler; the `.inc`/`.dec` handlers call `render()` but never `save()`.

### Summary
This build has bugs touching essentially every acceptance criterion except the "unknown code shows an error" half of AC3. Given this is attempt 1 of 3, recommend sending back to the developer with this list rather than merging.
