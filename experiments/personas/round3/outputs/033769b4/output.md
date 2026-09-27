## Not ready to merge — 8 acceptance-criteria failures found

`npm test` passing is misleading: the 4 unit tests only exercise happy paths and small amounts, so they miss every bug below. I verified each live with Playwright against the running app.

**Criterion 2 — Remove/empty state (broken):** `app.js` toggles a class `is-hidden` on `#summary`, but that class doesn't exist in `cart.css`. Emptying the cart shows "Your cart is empty" *alongside* a fully visible Order summary and Checkout button (screenshot: subtotal/total still show $0.00, Checkout still clickable). `public/app.js:52`

**Criterion 1 — Stepper bounds (broken):** `increment`/`decrement` in `cart.js` use `<=`/`>=` instead of `</>`, so qty can go to **0** (click − once at qty=1) and **11** (12 clicks of +) before the button finally disables — one step too late in both directions. The `disabled` conditions in `app.js` are similarly off-by-one. `public/cart.js:25-33`, `public/app.js:31-33`

**Criterion 3 — Promo code (two bugs):**
- Discount is stored as a frozen cents amount at apply-time and never recalculated, so it does **not** "follow later quantity changes" as required — confirmed: applied SAVE10 ($105.30), then incremented qty, discount stayed at $105.30 while subtotal moved to $1,182.00. `public/app.js:69`
- "Code not recognised" is never cleared when a subsequent valid code is applied — the error message stays on screen after successfully applying SAVE10. `public/app.js:63-72`

**Criterion 4 — Money format (broken):** `formatMoney` has no thousands separator. Default cart subtotal renders `$1053.00` instead of `$1,053.00`. `public/cart.js:13-15`

**Criterion 5 — Layout (two bugs):**
- `.summary` has an unconditional `min-width: 400px`, so at 375px the page horizontally scrolls (scrollWidth 420 vs viewport 375), violating "nothing scrolls horizontally." `public/cart.css:32`
- Breakpoint is `min-width: 769px`, so at exactly 768px (which the ticket names as the threshold) the layout is still stacked, not two-column. `public/cart.css:47`

**Criterion 6 — Persistence (broken):** `save()` is only called from the Remove handler, not from the inc/dec handlers, so quantity changes are lost on reload (confirmed: qty 1→2, reload, back to 1). Only removals persist. `public/app.js:37-38`

**Bonus, not in the criteria list but visible in every screenshot:** the "hidden" `#discount-row` div is always rendered because `.totals div { display: flex }` in the CSS has higher specificity than the browser's default `[hidden]{display:none}`, so "Promo (SAVE10) −$0.00" is shown even with no promo applied and an empty cart.

None of this is a nitpick pile-up — items 1, 2, 3, and 6 are direct, testable violations of explicit acceptance criteria, and 2/6 (empty state, persistence of qty) are the kind of thing a user would hit immediately. I'd send this back rather than merge. Want me to go ahead and fix these?
