I read through the whole front end (`index.html`, `css/app.css`, and all of `js/`). It's a clean, small vanilla-JS SPA with no build step. The escaping/XSS hygiene is solid (`escapeHtml` used consistently, dialog text set via `.textContent`), and error handling/network timeouts are already thought through. But there are several things I'd fix before launch, especially around keyboard/screen-reader accessibility — this is a healthcare booking flow, so those aren't cosmetic.

**Blockers**

1. **The specialty dropdown is unusable by keyboard.** `js/components/specialty-select.js` builds the trigger as a plain `<div class="select-trigger">` with only a `click` listener — no `tabindex`, no `role`, no keydown handling. A keyboard-only user can't open it at all. Needs to be a real `<button>` (or add `role="button" tabindex="0"` + Enter/Space/Escape/Arrow key handling) with `aria-expanded`/`role="listbox"`/`role="option"` wiring.

2. **Tab order is broken on the main page.** `js/views/clinicians.js:38` sets `tabindex="1"` on the search input. Any positive tabindex jumps to the front of the tab sequence, ahead of your own "Skip to main content" link and the header nav — so keyboard users tabbing from the top land on the search box first, defeating the skip link. Just remove the `tabindex="1"`.

3. **No visible focus indicator on time-slot buttons.** `css/app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no `:focus-visible` replacement (unlike `.btn`, which does this correctly). Keyboard users tabbing through appointment times can't see which one they're on — a real problem for the core interaction of the app.

4. **Form errors aren't exposed to assistive tech.** `js/views/checkout.js` (`showErrors`) adds visual `.invalid` styling and a `<p class="field-error">`, but never sets `aria-invalid` on the input or `aria-describedby` pointing at the error message. Screen reader users focused on a broken field won't hear why it failed.

**Should fix**

5. **No focus management on route change.** Only `confirmation.js` moves focus to its heading. `clinicians.js`, `slots.js`, and `checkout.js` don't move focus or update `document.title` on navigation, so SPA route changes are silent/invisible to screen reader and keyboard users beyond the scroll-to-top.

6. **Auto-advancing carousel has no pause control.** `tips-carousel.js` rotates every 5s forever with no way to stop/pause it — fails WCAG 2.2.2 and is just annoying if someone's trying to read a tip.

7. **Confirm-hold dialog doesn't manage focus/trap.** `confirm-dialog.js` unhides the modal (`aria-modal="true"`) but never moves focus into it and has no Escape-to-cancel or focus trap — Tab can escape behind the backdrop.

**Worth a look**

8. **Timezone mismatch risk in slot grouping** (`slots.js:19`, `groupByDay`): days are grouped by `slot.start.slice(0, 10)` (raw ISO date), but times are displayed via `new Date(...)` in the browser's local zone. If the API returns UTC and a user is far from the clinic's timezone, a slot could be filed under the wrong day heading relative to its displayed local time.
9. **Toast is the only feedback for "someone just took that time"** (`slots.js:124`) and disappears after 2s — easy to miss. Consider a longer duration or putting the message in the page instead of only a toast.
10. Minor: no `<meta name="description">`, and the checkout submit race (hold-expiry timer firing while a `createBooking` request is still in flight) isn't guarded — narrow edge case, low priority.

Nothing here touches the backend/API contract — that all looked sound (timeouts, `ApiError` with status codes, `credentials: 'same-origin'`). Want me to go ahead and fix the four blockers?
