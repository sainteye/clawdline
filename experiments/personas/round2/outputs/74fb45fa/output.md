I reviewed `index.html` plus everything it pulls in (`css/app.css`, and all of `js/`). Here's the punch list before launch, worst first.

## Accessibility blockers

1. **Specialty filter is unusable by keyboard.** `js/components/specialty-select.js:5-14` builds the trigger as a plain `<div>` with no `tabindex`, `role`, or key handling — only a click listener (line 48). Keyboard-only and screen-reader users cannot open it or pick a specialty at all. Needs `role="button"`/real `<button>`, `aria-expanded`, `aria-haspopup="listbox"`, and Enter/Space/Arrow/Escape handling.

2. **No visible focus indicator on time-slot buttons.** `css/app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no `:focus-visible` replacement (unlike `.btn`, which does have one at line 70-72). Keyboard users tabbing through appointment times can't see which slot is focused — this is the core booking action, so it's a real blocker.

3. **Confirm-hold dialog doesn't manage focus.** `js/components/confirm-dialog.js:33-48` toggles `hidden` and adds `aria-modal="true"`, but never moves focus into the dialog on open, never traps Tab inside it, and has no Escape-to-close handler. Keyboard/screen-reader users can tab out into the page behind a supposedly modal dialog.

4. **Auto-advancing health tips carousel has no pause control.** `js/components/tips-carousel.js` rotates every 5s forever with no way to stop/pause it (hover, focus, or a button). This is a straight WCAG 2.2.2 failure and worth fixing given it's a clinic site likely subject to ADA/accessibility requirements.

5. **`tabindex="1"` on the search input** (`js/views/clinicians.js:38`) — explicit positive tabindex values are an anti-pattern that breaks natural tab order as soon as another positive tabindex shows up anywhere. Just remove it.

## Correctness / robustness

6. **No busy-lock between confirming a hold and the `holdSlot` call.** `js/views/slots.js:110-127` — after the user confirms in the dialog, nothing disables the slot list while `holdSlot` is in flight, so a second click can kick off a second confirm/hold cycle before the first resolves. Worth disabling the slot grid (or at least the clicked button) until the request settles.

7. **Error messages aren't wired to inputs for assistive tech.** `js/views/checkout.js:44-56` (`showErrors`) adds a visual `.field-error` and `.invalid` class but never sets `aria-invalid` or `aria-describedby` on the input, so a screen-reader user tabbing to the invalid field won't hear why it's invalid.

8. **Date-of-birth input has no `max` attribute.** `js/views/checkout.js:78`ish (`FIELDS` entry) only blocks future dates in JS after submit; add `max="<today>"` to the `<input type="date">` so the native picker itself won't offer future dates.

## Polish / pre-launch checks

9. **Document title never changes between routes.** `js/main.js:40-65` (`render()`) leaves `<title>` fixed at "Book an appointment…" for every view, including the confirmation page. Screen readers and browser history both rely on title changes; set it per-route.

10. **Confirm the non-hash links actually resolve.** `index.html:20` (`/help`), plus `/tips` and `/policy` referenced from the JS — these are real server paths, not part of the SPA's hash router. If this is a static single-page deploy, double check those routes exist server-side or they'll 404 at launch.

11. **Missing `<meta name="description">` and favicon** in `index.html` — cosmetic/SEO, but cheap to add before launch.

Nothing here is an XSS risk — every dynamic string I traced goes through `escapeHtml()` (`js/format.js:63-70`) consistently across all views, which is good.

Want me to go ahead and fix any of these — I'd start with #1–#4 since those block real users from completing a booking?
