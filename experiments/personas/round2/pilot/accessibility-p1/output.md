I read through the whole flow (`index.html`, `main.js`, `state.js`, `api.js`, all components/views, `app.css`) rather than just the shell page, since the actual booking UI lives in the JS modules it loads. Overall the code is careful about XSS (consistent `escapeHtml` use) and has decent defensive patterns (request-id guards, hold-expiry handling). Here's what I'd fix before launch, in priority order.

## Blockers (accessibility/functional breakage)

1. **Specialty filter is unusable by keyboard.** `js/components/specialty-select.js:13` renders the dropdown trigger as a plain `<div class="select-trigger">` with only a click handler — no `tabindex`, no `role="button"`, no keyboard handler. Keyboard-only users cannot focus it, open it, or change the specialty filter at all. Either use a native `<select>` or implement a proper ARIA listbox/combobox pattern (focusable trigger, arrow-key navigation, Enter/Escape).

2. **Confirm-hold dialog has no focus trap or Escape handling.** `js/components/confirm-dialog.js:33-48` + `index.html:53-67` mark it `role="dialog" aria-modal="true"`, but opening it never moves focus into the dialog, Tab can reach page content behind the backdrop, and Escape does nothing. For a dialog that gates a 5-minute hold, this needs real focus management (focus the default button on open, trap Tab inside, Escape = cancel, return focus to the triggering slot button on close).

3. **No focus indicator on time-slot buttons.** `css/app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no `:focus-visible` replacement (unlike `.btn`, which does have one at `app.css:70-72`). Keyboard users tabbing through available times see nothing highlighted — they can't tell which slot they're about to activate.

4. **Route changes don't move focus.** `js/main.js:40-65` scrolls to top on navigation but never focuses the new view's heading, except `confirmation.js:29` which does it right. Screen reader and keyboard users navigating clinicians → slots → checkout get no cue that content changed; focus is left dangling wherever it was (often on a now-removed element). Apply the same `heading.focus()` pattern used in `confirmation.js` to every view.

5. **Auto-rotating health tips carousel has no pause control.** `js/components/tips-carousel.js` advances every 5s forever with no stop/pause button and no `prefers-reduced-motion` check (`css/app.css:196` transition always runs). This fails WCAG 2.2.2 (content that auto-updates past 5s needs a way to pause/stop it) and will bother motion-sensitive users.

## Should fix

6. **Availability dots carry no accessible name.** `js/views/clinicians.js:22-23` renders `<span class="availability availability--${level}">` with no text and no `aria-label`. Screen reader users get zero signal about "many times / a few times / fully booked" per clinician — only sighted users get it, and only via a legend elsewhere on the page. Add `aria-label` (or visually-hidden text) with the actual status per card.

7. **Hardcoded `tabindex="1"` on the search box.** `js/views/clinicians.js:38`. Positive tabindex values override natural DOM order for the whole page, which is fragile and will bite you the moment header/nav content changes. Remove it and rely on source order.

8. **Nav/footer links point to routes the SPA doesn't own.** `index.html:20` (`/help`), `js/views/checkout.js:78` (`/policy`), `index.html:49` (`/tips`) are plain paths, not hash routes, so they trigger full navigations away from this single-page app. Confirm these are real server-rendered pages before launch — if they're not, users hit a 404 or blank screen.

## Worth doing, not blocking

- Add an empty-state message when a clinician search returns 0 results (`clinicians.js:63-70` just shows "0 clinicians found" with a blank list).
- Show a loading/disabled state on the slot button between confirming the hold dialog and the `holdSlot` request resolving (`slots.js:110-127`) — currently silent if the network is slow.
- Consider firing `releaseHold` on `pagehide`/`beforeunload` in checkout (`checkout.js`) so an abandoned tab frees the slot for other patients sooner instead of waiting for server-side TTL.
- Set a `max` attribute (today's date) on the date-of-birth input in `checkout.js:35` so the native picker doesn't offer future dates that you then reject after submit.

The specialty-select keyboard trap (#1) and the missing focus management on dialog/route changes (#2, #4) are the ones I'd treat as真 launch blockers — they make core booking actions impossible or disorienting for keyboard/screen-reader users, not just rough edges.
