I read through `index.html`, `css/app.css`, and every file under `js/` (routing in `main.js`, state, API layer, and each view/component). No test harness, axe config, or browser-automation tool is set up in this repo, and I don't have a real screen reader (VoiceOver/NVDA) driver available in this CLI session, so this isn't a live AT transcript — it's a careful trace of the actual markup, ARIA wiring, and event-handler code for every view, checked against what a keyboard-only user could reach and what a screen reader would actually expose. That's enough to be confident about the findings below; say the word if you want me to spin up a local server and run an automated scan (e.g. axe via Playwright) on top of this — that would need installing packages, so I'd check with you first.

## Blocking before launch

**1. Route changes never move focus — affects every step but the last one**
`main.js:40-65` swaps `#view`'s contents on every hash change but never calls `.focus()` on anything. Only `confirmation.js:29` does it right (`#done-heading.focus()`). `clinicians.js`, `slots.js`, and `checkout.js` don't. A screen-reader user who activates "View times," picks a slot, or cancels a hold gets no announcement that anything happened — focus silently drops to `<body>` and they have to re-explore the page from scratch at every step.
- **WCAG 2.4.3 Focus Order (A)**, related to **4.1.3 Status Messages (AA)**.
- Fix: in `render()`, after awaiting the view, focus the new heading (add `tabindex="-1"` like `confirmation.js` already does) or focus the view container itself.

**2. Time-slot buttons have no visible keyboard focus indicator**
`app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no `:focus-visible` replacement, and the buttons in `slots.js:26-43` carry no `.btn` class, so they never get the `:focus-visible` ring defined at `app.css:70-72` either. A keyboard user tabbing through appointment times has zero visual indication of which slot they're about to activate — for a *health appointment*, picking the wrong time because focus was invisible is a real-world consequence, not an inconvenience.
- **WCAG 2.4.7 Focus Visible (AA)**.
- Fix: give these buttons a focus-visible style (reuse the `.btn:focus-visible` box-shadow pattern).

**3. Specialty filter is a `<div>`-based dropdown with no keyboard or screen-reader support at all**
`specialty-select.js` builds the whole control from `<div class="select-trigger">` and `<li class="select-option">` with only `click` listeners (lines 28-58). No `tabindex`, no `role`, no `aria-expanded`, no `aria-haspopup`, no keydown handling. A keyboard-only user cannot even focus the trigger — Tab skips straight over it — and a screen reader has nothing to announce (a `div` has no implicit role or name).
- **WCAG 2.1.1 Keyboard (A)** and **4.1.2 Name, Role, Value (A)**.
- This doesn't block booking entirely (default "All specialties" plus name search still works), but it silently removes a core filter for every keyboard/AT user.
- Fix: rebuild as a native `<select>`, or a proper ARIA listbox/combobox per the [APG pattern](https://www.w3.org/WAI/ARIA/apg/patterns/listbox/) with `aria-expanded`, `aria-controls`, arrow-key/Enter/Escape handling.

**4. "Hold this time?" confirm dialog doesn't behave like the modal it claims to be**
`confirm-dialog.js:33-48` sets `el.hidden = false` but never moves focus into the dialog, never traps it, and has no Escape handler. The dialog markup (`index.html:53-67`) declares `role="dialog" aria-modal="true"`, which tells assistive tech the rest of the page is inert — but nothing outside the dialog is actually hidden or made `inert`. A keyboard user pressing Enter on a slot button stays focused on that (now hidden-behind-overlay) button and has to Tab through the rest of the page's remaining content before ever reaching "Hold and continue" or "Choose another time."
- **WCAG 2.4.3 Focus Order (A)**, **4.1.2 Name, Role, Value (A)** (the ARIA-declared modal contract isn't honored).
- Fix: on open, focus the dialog (or its first button); on close, return focus to the slot button that opened it; add Escape → cancel; add `inert` (or `aria-hidden`) to the rest of the document while open.

## Should fix before launch

**5. Availability indicator on clinician cards is color-only with no text alternative**
`clinicians.js:23` — `<span class="availability availability--${level}"></span>` — is empty. Screen readers get nothing (no name at all), and the green/amber/red distinction (`app.css:128-130`) is hard to read for red-green colorblind users, which is a meaningful fraction of your patient population.
- **WCAG 1.1.1 Non-text Content (A)** and **1.4.1 Use of Color (A)**.
- Fix: add visually-hidden text or `aria-label` inside the span, e.g. "Many times available."

**6. Field errors on checkout aren't linked to their inputs for assistive tech**
`checkout.js:44-56` appends a sibling `<p class="field-error">` and adds a `.invalid` class, then focuses the first bad field — good instinct, but the input has no `aria-invalid="true"` and no `aria-describedby` pointing at the error text, so a screen reader landing on the field via `.focus()` won't necessarily read *why* it's invalid.
- **WCAG 1.3.1 Info and Relationships (A)**, **3.3.1 Error Identification (A)**.
- (Credit where due: the error copy itself is genuinely good — "Enter a phone number... for example 555-010-0200" — that's real 3.3.3 Error Suggestion quality, it just isn't wired up programmatically.)
- Fix: give each error `<p>` an `id`, set `aria-describedby` and `aria-invalid="true"` on the matching input, clear both in `clearErrors`.

**7. Time-slot button names are ambiguous outside their visual grouping**
Each button's only accessible name is the time (`slots.js:31-33`, e.g. "9:00 AM"), disambiguated only by a `<h3>` day heading above it. In a screen reader's flat "buttons" list (a common navigation mode), a week of slots reads as a wall of repeated "9:00 AM," "9:00 AM," "10:00 AM"… with no day attached.
- Best-matched to **WCAG 2.4.6 Headings and Labels (AA)**.
- Fix: include the day in the accessible name, e.g. `aria-label="9:00 AM, Monday, October 6"`.

**8. Health-tips carousel auto-advances forever with no way to stop it**
`tips-carousel.js:1,23` rotates every 5 seconds indefinitely; there's no pause/stop control anywhere in `index.html`. This is a clean, unambiguous hit.
- **WCAG 2.2.2 Pause, Stop, Hide (A)**.
- Fix: add a visible pause/stop control (and ideally respect `prefers-reduced-motion`).

## Worth fixing, lower stakes

- **`clinicians.js:38`** — `tabindex="1"` on the search input is a positive-tabindex anti-pattern that reorders the tab sequence independent of visual/DOM order (**2.4.3 Focus Order, A**). Just delete it; natural order already puts the search box first.
- **`toast.js`** — toasts auto-dismiss after 2s with no close/replay affordance, and the slot-conflict error in `slots.js:124` ("Someone just took that time…") only goes through `role="status"`/`aria-live="polite"`, not an assertive interruption. If a user looks away for two seconds they lose the only explanation for why their selection was cleared. Consider a longer duration (or manual dismiss) for error toasts specifically, and `aria-live="assertive"`/`role="alert"` for conflict/expiry messages.
- **Advisory, not a clear violation** — the 5-minute hold in `checkout.js` has no warning before expiry and no way to extend it; it plausibly falls under 2.2.1's "real-time exception" (protecting a contested shared resource), but a "1 minute left" announcement before the countdown clears the hold would be a meaningful, cheap improvement for anyone who reads slowly.

## What already works — keep it

- Skip link (`index.html:11`, `app.css:36-37`) is implemented correctly.
- `#results-count` (`clinicians.js:47`) and `#week-label` (`slots.js:66`) are properly `aria-live="polite"` and do announce search/week changes.
- Confirmation page (`confirmation.js:13,29`) correctly moves focus to a `tabindex="-1"` heading — this is the pattern the other three views should copy.
- Confirm dialog's `aria-labelledby`/`aria-describedby` wiring is correct, and the body text is set before the dialog is revealed.
- Tips carousel correctly removes inactive slides from the tab order and marks them `aria-hidden` (`tips-carousel.js:15-18`) — the only thing missing is the pause control (#8 above).
- Consent checkbox's link includes "(opens in a new tab)" as real label text (`checkout.js:78`) — exactly right.
- No viewport zoom-lock, and font sizes are `rem`-based throughout — 200% zoom / text-only zoom should work fine.
- Text color contrast is solid everywhere I spot-checked (nav links, muted meta text, error text, primary-button text all landed at 6:1 or better against their backgrounds).

## Bottom line

This does not currently meet WCAG 2.1 AA. Items 1–4 sit directly in the primary booking flow (pick a clinician → pick a time → confirm hold → check out) and I'd treat them as launch blockers; 5–8 are real Level A/AA failures but don't stop task completion. None of this requires a redesign — every fix is markup/ARIA/CSS on components that already exist.
