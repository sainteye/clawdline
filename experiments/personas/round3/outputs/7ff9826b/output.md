# QA Verification — TICKET-88 (Editable cart page), Attempt 1

Environment: app served locally via `npm start` at `http://127.0.0.1:4173/`. `npm test` run at commit state of the working tree. Interaction evidence captured with `playwright-core` (Chromium headless) via two scripts left in `scripts/interact-qa.mjs` and `scripts/interact-qa2.mjs`; screenshots and raw logs saved under `/tmp/qa-88/`.

## 1. Criterion-by-criterion results

| # | Criterion (verbatim) | Result | Evidence | Notes |
|---|---|---|---|---|
| 1 | Quantity stepper: − disabled at 1, + disabled at 10, qty stays 1–10, totals update immediately | **FAIL** | `node scripts/interact-qa2.mjs` → `qty after underflow click: 0`; `stepper-underflow.png` (qty shown as `0`, line total `$0.00`). Also `node scripts/interact-qa.mjs` → 9 inc clicks → qty `10`, `inc disabled at qty=10? false`, 10th click → qty `11`, disabled only *after* the overflow; `stepper-overflow.png` shows qty `11`. | Off-by-one in `public/app.js:31,33` (`item.qty < MIN_QTY` / `item.qty > MAX_QTY` for the `disabled` attribute) combined with `public/cart.js` `increment`/`decrement` using `<=`/`>=` instead of `<`/`>`. Totals *do* update immediately — that half works. |
| 2 | Remove deletes a line; removing the last line shows "Your cart is empty" and hides the whole order summary incl. Checkout | **FAIL** | `empty message hidden? false`, `empty message text: "Your cart is empty."` (text matches), but `checkout visible? true` after emptying the cart; `empty-cart.png` shows Order summary card and the Checkout button still fully rendered with `$0.00` totals. | `public/app.js:52` toggles class `is-hidden` on `#summary`, but `public/cart.css` defines no `.is-hidden` rule at all (`grep is-hidden public/cart.css` → no matches), so nothing is actually hidden. |
| 3 | `SAVE10` (case-insensitive) −10% on subtotal, discount follows later qty changes; unknown code shows "Code not recognised"; valid code clears that message | **FAIL** | Unknown-code message: `promo-error text for BOGUS: "Code not recognised"`, correctly shown (`promo-unknown-code.png`). Discount tracking: after applying SAVE10 at subtotal $1053.00 → discount `−$105.30`; after bumping qty (subtotal → `$1440.00`), discount stayed `−$105.30` instead of `−$144.00` (`promo-after-qty-change.png`). Error-clearing: after BOGUS then SAVE10, `promo-error hidden after valid code? false` — message "Code not recognised" is still visible even though the 10% discount was correctly applied (`promo-valid-after-invalid.png`). | Two independent bugs: (a) `state.discount` in `public/app.js` is a frozen cent amount computed once at apply-time, never recomputed from current subtotal; (b) the submit handler never clears `state.promoError` in the success branch. |
| 4 | Money in USD with thousands separator and two decimals, e.g. `$1,234.50` | **FAIL** | `desktop-1280.png`: Subtotal/Total render as `$1053.00`, not `$1,053.00`. Also seen in every other screenshot with a 4-digit total (e.g. `stepper-overflow.png` shows `$2343.00` and `$1419.00`). | `formatMoney` in `public/cart.js:14` is `'$' + (cents/100).toFixed(2)` — no grouping. `npm test`'s `formatMoney` test only checks `1250` (`$12.50`) and `5` (`$0.05`), so it never exercises a 4-digit amount and doesn't catch this. |
| 5 | ≥768px: summary to the right; <768px: stacked; 375px: no horizontal scroll, Checkout fully visible | **FAIL** | At exactly 768px: `items box {x:20,w:728}`, `summary box {x:20,y:321}` → summary is below, not right of, items (`768px summary right of items? false`); `layout-768-check.png` confirms visually. At 769px the same check is `true`. At 375px: `document.documentElement.scrollWidth = 420` vs `clientWidth = 375` (horizontal overflow of 45px); `.checkout` bounding box `{x:41, width:358}` → right edge 399 > viewport width 375, so `checkout fully within viewport width? false`; independently, `node scripts/shot.mjs http://127.0.0.1:4173/ 375 ... ` produced a PNG that is **420×900 px**, not 375 px wide, confirming the overflow. | Root cause of both sub-bugs: `public/cart.css:32` sets `.summary { min-width: 400px; ... }` unconditionally (outside the `@media (min-width: 769px)` block at line 47), which both forces the mobile layout to overflow horizontally and pushes the real breakpoint to 769px instead of the documented 768px. |
| 6 | After reload, cart shows same items and quantities | **FAIL for quantities** (items/removal persist) | Fresh context, 2 stepper clicks on line 0 → qty shown `3`; `localStorage.getItem('cart:v1')` immediately after = `null`; after `page.reload()`, qty reverts to `1` (`AC6: qty after reload: 1`). Separately, removal *does* persist since `remove` calls `save()` (verified by code read; not independently screenshotted since it wasn't in question). | `public/app.js` inc/dec handlers (lines 37–38) call `render()` but never `save()`; only the `remove` handler (line 41) calls `save()`. Any quantity change made via the steppers is lost on reload. |

`npm test` output (for reference): 4/4 tests pass — this matches the developer's claim, but the suite doesn't cover stepper boundaries through the DOM, discount recompute, the promo-error-clear path, the money format at 4 digits, or persistence via the steppers, so a green `npm test` does not indicate these criteria are met.

## 2. Issues found

**Issue A — Quantity can go outside 1–10 (AC1)**
- Repro: load page → click "−" on a line at qty 1 (e.g. via `page.locator('.line').first().locator('.dec').click({force:true})`, or just click fast in the UI since the button isn't actually disabled at qty 1).
- Expected: "−" is disabled at qty 1, qty never drops below 1.
- Actual: qty drops to 0, line total shows `$0.00`; only *then* does the button become disabled. Symmetric bug on the top end: "+" isn't disabled until qty reaches 11, so a 10th click on a fresh cart pushes qty to 11 before disabling.
- Evidence: `/tmp/qa-88/stepper-underflow.png`, `/tmp/qa-88/stepper-overflow.png`, `/tmp/qa-88/interact.log`, `/tmp/qa-88/interact2.log`.

**Issue B — Empty-cart state doesn't hide the order summary (AC2)**
- Repro: remove all three lines.
- Expected: "Your cart is empty" shown, order summary (subtotal/discount/total/promo form/Checkout) hidden.
- Actual: empty message shows correctly, but the entire Order summary card, including a clickable Checkout button, remains visible showing `$0.00` totals.
- Evidence: `/tmp/qa-88/empty-cart.png`; `grep is-hidden public/cart.css` → no rule defined.

**Issue C — Discount doesn't follow later quantity changes (AC3)**
- Repro: apply `SAVE10`, note discount; increase a line's quantity; observe discount unchanged.
- Expected: discount recalculates as 10% of the new subtotal.
- Actual: discount stays frozen at the value computed at apply-time ($105.30) even after subtotal rises to $1,440.00 (correct discount would be $144.00); total is therefore wrong ($1,334.70 shown vs. $1,296.00 expected).
- Evidence: `/tmp/qa-88/promo-after-qty-change.png`, `/tmp/qa-88/interact.log`.

**Issue D — "Code not recognised" message isn't cleared by a later valid code (AC3)**
- Repro: apply an unknown code (e.g. `BOGUS`), then apply `SAVE10`.
- Expected: error message disappears once the valid code is applied.
- Actual: discount is applied correctly, but "Code not recognised" remains visible underneath the field.
- Evidence: `/tmp/qa-88/promo-valid-after-invalid.png`.

**Issue E — No thousands separator in money format (AC4)**
- Repro: load the default cart (subtotal $1,053.00).
- Expected: `$1,053.00`.
- Actual: `$1053.00` everywhere a 4-digit amount appears.
- Evidence: `/tmp/qa-88/desktop-1280.png`, `/tmp/qa-88/stepper-overflow.png` ($2343.00, $1419.00); code: `public/cart.js:14`.

**Issue F — Responsive breakpoint is 769px, not 768px, and the layout overflows horizontally at 375px (AC5)**
- Repro (breakpoint): load at exactly 768px width.
- Expected: summary to the right of the item list.
- Actual: summary stacks below (only switches to side-by-side at 769px). Evidence: `/tmp/qa-88/layout-768-check.png`, bounding-box measurements in `/tmp/qa-88/interact2.log`.
- Repro (overflow): load at 375px width.
- Expected: no horizontal scroll; Checkout fully visible.
- Actual: `document.documentElement.scrollWidth` is 420px against a 375px viewport (45px overflow), and the full-page screenshot at "375px" actually renders 420px wide; Checkout's right edge (399px) sits outside the 375px viewport. Root cause: unconditional `min-width: 400px` on `.summary` in `public/cart.css:32`.
- Evidence: `/tmp/qa-88/mobile-375.png` (420×900 px file), `/tmp/qa-88/interact2.log`.

**Issue G — Quantity changes made via the steppers are not persisted (AC6)**
- Repro: change a line's quantity with +/−, reload the page without removing anything.
- Expected: quantity unchanged after reload.
- Actual: `localStorage['cart:v1']` is `null` right after stepper clicks; reload reverts the quantity to its original value.
- Evidence: `/tmp/qa-88/interact2.log` ("AC6: ..." lines); code: `public/app.js:37-38` (inc/dec handlers never call `save()`).

## 3. Verdict

**FAIL — needs work.**

5 of 6 acceptance criteria fail in the running app (only "remove deletes a line" and "unknown code shows the message" sub-parts of AC2/AC3 behave correctly; everything else in this ticket has at least one confirmed defect: stepper bounds, empty-state hiding, discount recompute, error-message clearing, money formatting, the 768px breakpoint, 375px horizontal overflow, and stepper-driven persistence). `npm test` passing and a manual Chrome check are not sufficient evidence for this ticket — the existing unit tests don't exercise DOM-level boundary behavior, 4-digit amounts, or the persistence/discount-recompute paths where the bugs live.
