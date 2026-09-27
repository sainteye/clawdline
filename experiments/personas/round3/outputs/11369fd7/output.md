## Evidence report — TICKET-88

**Environment:** `node server.mjs` (port 4173) serving `public/` as-is, no build step; driven with `playwright-core` (Chromium headless), Node v24.1.0, macOS. All checks below used a cleared `localStorage` unless noted.

**`npm test`** — passes as claimed:
```
✔ subtotal of the default cart
✔ increment and decrement change the quantity
✔ SAVE10 gives 10 percent off, any case
✔ formatMoney shows dollars and cents
tests 4, pass 4, fail 0
```
Note: the existing "increment and decrement" unit test only exercises qty 2→3→1, never the 1/10 boundary, so it doesn't catch what's below.

---

**Claim: "Quantity stays between 1 and 10; − disabled at 1, + disabled at 10"**
Script: loaded page, cleared storage, reloaded, clicked `.dec` once on the line starting at qty 1.
```
initial qty line0: 1  dec disabled: false
after dec click 1: qty= 0  dec disabled: true
```
Repeating `+` 9 times on the qty-2 line produced `qty=11, inc disabled=true`.
Screenshot `qty-zero.png`: keyboard line shows quantity `0`, line total `$0.00`, `−` now greyed out. `public/cart.js` disables only after crossing the bound (`item.qty <= MAX_QTY` / `>= MIN_QTY` before mutating), so qty briefly reaches 0 or 11.

**Claim: "Removing the last line ... hides the whole order summary, including the Checkout button"**
Script: removed all three lines, then read the summary's computed style.
```
empty message visible= true
summary has is-hidden class= true  summary still visible= true  computed display= block
```
Screenshot `empty-state.png`: "Your cart is empty." shows, but the Order summary card (Subtotal $0.00, Checkout button) is still fully rendered beside it. `app.js` toggles a CSS class `is-hidden` on `#summary`, but `public/cart.css` contains no `.is-hidden` rule (`grep is-hidden public/cart.css` → no match).

**Claim: "Applying a valid code removes [the 'Code not recognised'] message" / "discount follows later quantity changes"**
Script: submitted `NOPE`, then `SAVE10`.
```
after invalid code, error visible= true text= "Code not recognised"
after valid code, error still visible= true discount= −$105.30
```
Screenshot `promo-error-after-valid.png` shows the red "Code not recognised" text still under the field even though SAVE10 was just accepted and the discount row shows −$105.30.

Then, with SAVE10 already applied, incremented the monitor's qty by one:
```
discount before qty change= −$105.30  total before= $947.70
discount after qty change=  −$105.30  total after= $1846.70
```
The discount amount is frozen at the value computed when the code was submitted; it does not rescale with the new subtotal.

**Claim: money shown "with a thousands separator", e.g. `$1,234.50`**
Default cart subtotal on load:
```
subtotal text = "$1053.00"   total text = "$1053.00"
```
No comma is inserted (`formatMoney` in `public/cart.js` is `'$' + (cents/100).toFixed(2)`); this reproduces on the very first page load with no interaction needed.

**Claim: "At widths of 768 px and above, the summary sits to the right"; "At 375 px nothing scrolls horizontally and the Checkout button is fully visible"**
```
WIDTH 768: flex-direction=column, horizontalOverflow=false
WIDTH 769: flex-direction=row,    horizontalOverflow=false
WIDTH 375: flex-direction=column, scrollWidth=420, clientWidth=375, horizontalOverflow=true
```
Screenshot `layout-768.png`: at exactly 768px the summary is still stacked below the item list (`cart.css` media query is `min-width: 769px`, one pixel off from the ticket's "768 px and above").
Screenshot `layout-375.png`: the page is 420px wide inside a 375px viewport (visible as a white strip cut off the top-right of the header bar), i.e. horizontal scrolling is present. This traces to `.summary { min-width: 400px; }` in `cart.css`, which has no override for narrow widths.

**Claim: "After a reload the cart shows the same items and quantities"**
Script: incremented the monitor's qty from 1 to 3, then reloaded.
```
qty before reload = 3
qty after reload = 1
localStorage cart:v1 = null
```
`localStorage` is untouched by quantity changes — in `public/app.js`, the `.inc`/`.dec` click handlers call `render()` but never `save()` (only the `remove` handler does), so quantity edits are lost on reload.

**Not captured:** I did not test Safari/Firefox (only Chromium), and did not exercise keyboard/screen-reader interaction beyond what's shown above (aria-live output, aria-labels) — the note only claimed a Chrome check, so this is consistent with that, not an added gap.

---

I'm not rendering a pass/fail call — that's yours to make — but for reference, here's what the evidence lines up against each acceptance criterion: stepper bounds (violated), remove/empty state (violated — summary never hides), promo code (violated — error doesn't clear, discount doesn't follow qty), money format (violated — no thousands separator), layout (violated at both 768px and 375px), persistence (violated for quantity changes). Only the SAVE10 case-insensitivity and the initial promo-error message itself matched spec on their own.

All screenshots are in `/tmp/ticket88-evidence/` (`qty-zero.png`, `empty-state.png`, `promo-error-after-valid.png`, `layout-768.png`, `layout-769.png`, `layout-375.png`) if you want to look at them directly. Temporary check scripts and the dev server have been cleaned up.
