Here is the evidence package for TICKET-88, checked against `docs/TICKET-88.md`. Build: the working tree as provided (not a git repo, no commit hash available). Server: `node server.mjs` on `http://127.0.0.1:4173/`. Tool: `playwright-core` (Chromium headless), driven via one-off scripts run from `scripts/` (removed after use).

**Claim: "`npm test` passes."**
Command `npm test` → 4/4 pass (`subtotal`, `increment/decrement`, `SAVE10`, `formatMoney`). Raw output captured above. Note: none of these tests exercise the qty=1/qty=10 boundary or amounts ≥ $1,000, which is why the issues below aren't caught by the suite.

**Claim 1 — Quantity stepper stays between 1 and 10, disabled at the ends.**
Scripted clicks on line 1 (`public/cart.js:25-33`, `public/app.js:31,33`):
- `+` clicked repeatedly: qty went 1→2→…→10→**11**, only then did the button disable. Log: `click 9: qty now = 10` / `click 10: qty now = 11` / `click 11: + already disabled`.
- `−` clicked from qty=1: qty went to **0**, only then did the button disable. Log: `click 1: qty now = 0` / `click 2: - already disabled`.
Cause visible in code: `increment` guards with `qty <= MAX_QTY` (should be `<`) and the `+` button's `disabled` condition is `qty > MAX_QTY` (should be `>=`); symmetric off-by-one on `decrement`/`−` with `MIN_QTY`.

**Claim 2 — Removing the last line hides the whole summary including Checkout.**
Removed all 3 lines via `.remove`. `#empty` becomes visible with "Your cart is empty." (correct). But `#summary` only gets an `is-hidden` class — `cart.css` defines no `.is-hidden` rule (grep returned no matches), so the summary panel and Checkout button remain rendered and visible. Confirmed: `checkout link visible: true`. Screenshot `/tmp/ev_empty_state.png` shows Order summary, Subtotal $0.00, promo field, and a blue Checkout button still present.

**Claim 3 — Promo code SAVE10, error handling, and following quantity changes.**
- Unknown code `FREESHIP` → shows "Code not recognised" (correct).
- Then applying `save10` → discount/total compute correctly ($1053.00 subtotal, −$105.30, $947.70 total), **but** "Code not recognised" is still shown (`error hidden: false`). Screenshot `/tmp/ev_promo_error_persists.png`. Cause: the submit handler never clears `state.promoError` on success (`public/app.js:63-72`).
- Incrementing a line's qty afterward: subtotal moved to $1182.00 but discount stayed frozen at "−$105.30" (should be −$118.20) and total became $1076.70 instead of $1063.80. Cause: only the computed cents amount is stored (`state.discount`), not the rate, so it isn't recalculated on `render()` (`public/app.js:5-9,69`).

**Claim 4 — Money format with thousands separator, e.g. `$1,234.50`.**
Observed rendered values: `$1053.00` and `$1182.00` (no comma). `formatMoney` (`public/cart.js:13-15`) is `'$' + (cents/100).toFixed(2)` with no grouping logic.

**Claim 5 — Layout: two-column ≥768px, stacked <768px; no horizontal scroll at 375px with Checkout fully visible.**
- Computed `flex-direction` of `.cart`: 767px→`column`, **768px→`column`**, 769px→`row`. The CSS media query is `@media (min-width: 769px)` (`public/cart.css:47`), so exactly 768px (which the ticket says should be two-column) is still stacked.
- At 375px viewport: `document.documentElement.scrollWidth=420` vs `clientWidth=375` → horizontal overflow present. `.checkout` bounding box `{x:41, width:358}` → right edge at 399px, 24px past the 375px viewport edge. Cause: `.summary { min-width: 400px; ... }` (`public/cart.css:32`) applies at all widths and is only overridden inside the `min-width:769px` media query, so it still forces 400px+ width on mobile. Screenshot `/tmp/ev_375.png` shows the page (fullPage capture at 375×800); a sliver of white summary content is visible past the intended right edge.

**Claim 6 — Persistence across reload.**
- Removing an item: persists correctly. `localStorage['cart:v1']` populated immediately after remove, and the removed state survives `page.reload()` (2 lines before and after).
- Changing quantity: does **not** persist. After two `+` clicks (qty 1→3), `localStorage.getItem('cart:v1')` is still `null` before any reload. After `page.reload()`, qty reverts to `1`. Cause: the `.inc`/`.dec` click handlers in `public/app.js:37-38` call `render()` but never call `save()` (unlike the `.remove` handler at line 39-43, which does).

**What I could not test:** visual rendering nuance beyond what screenshots/computed styles show (e.g., subjective "looks good" polish), and no cross-browser check beyond Chromium (matches what Playwright's headless shell provides here; the developer's manual Chrome check would have been the same rendering engine).

That's the full evidence set — six distinct discrepancies from the acceptance criteria, each reproducible with the commands/scripts above. I'll leave the merge decision to you.
