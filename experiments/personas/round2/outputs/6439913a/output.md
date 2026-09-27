I read through the whole front end — `index.html`, `css/app.css`, and all of `js/` (routing, api, state, format helpers, and the four views/four components). The code is generally solid (consistent `escapeHtml` use, sensible API error handling, debounced search), but there are several things I'd fix before launch, roughly in priority order.

**Blocking bugs**

1. **Slot dates can render as the wrong day.** `js/views/slots.js:19,39` groups slots by `slot.start.slice(0, 10)` and formats that with `formatDay()`, which does `new Date(isoDate)` in `js/format.js:19`. A date-only string (`"2026-09-28"`) is parsed as UTC midnight; in any US timezone that becomes the *previous* evening locally, so the day headings can show the wrong date/weekday for appointment slots. The reloaded `weekStart` (`slots.js:47`) has the same UTC-vs-local mismatch when compared against `startOfWeek(new Date())` for the "Previous week" disabled state (`slots.js:81`). This is the kind of bug that's easy to miss in testing near UTC but breaks for real users.

2. **The specialty filter is unusable by keyboard.** `js/components/specialty-select.js:11-14,28-53` builds the dropdown trigger as a `<div class="select-trigger">` with only a `click` handler — no `tabindex`, `role="button"`, `aria-expanded`/`aria-haspopup`, or keyboard handling, and the option list items aren't real controls either. A keyboard-only user can't open or use this filter at all.

3. **The confirm dialog has no focus management.** `index.html:53-67` declares `role="dialog" aria-modal="true"`, but `js/components/confirm-dialog.js` never moves focus into the dialog on open, never traps Tab inside it, and has no Escape-to-close handler. For a modal that gates the actual booking action, that's a real accessibility gap (and a legal-risk one for a clinic site).

4. **Time-slot buttons have no visible focus indicator.** `css/app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no accompanying `:focus-visible` rule (unlike `.btn`, which correctly swaps to a box-shadow at lines 66-79). Keyboard users tabbing through appointment times get no visual indication of where they are — on the single most important control in the app.

**Should fix before launch**

5. **Availability dots convey meaning by color only.** In `js/views/clinicians.js:22-24`, each clinician's `<span class="availability--{level}">` has no text/`aria-label`. The shared legend explains the colors once, but nothing ties a specific dot back to "many/few/none" for assistive tech or colorblind users scanning the list. Add an `aria-label` or visually-hidden text per dot.

6. **Route changes don't announce themselves.** `confirmation.js:29` correctly moves focus to its heading, but `clinicians.js`, `slots.js`, and `checkout.js` don't — and `document.title` never changes between views (`index.html:6` is static). Screen-reader users get no cue that navigating to `#/checkout` etc. actually changed the page.

7. **Toasts vanish too fast for their content.** `js/components/toast.js:1,12` defaults to 2000ms, used for messages like "Someone just took that time. Please pick another." (`slots.js:124`). That's genuinely easy to miss, and there's no way to dismiss/re-read it. Consider a longer duration (or no auto-dismiss) for error toasts specifically.

8. **Tips carousel auto-rotates forever with no pause.** `js/components/tips-carousel.js` advances every 5s indefinitely with no pause/stop control and no `prefers-reduced-motion` check (transition is unconditional in `css/app.css:196`). This fails WCAG 2.2.2 (content that moves/updates automatically must be pausable).

**Polish / pre-launch checklist**

9. `index.html:3-8` has no `<meta name="description">`, no favicon, no Open Graph/social preview tags — worth adding before this is a public URL.
10. Confirm the footer contact info is real, not placeholder: `index.html:72` — phone `555-010-0200` and an address with no city/state/zip look like test data.
11. `checkout.js` — the reason `<textarea>` (line ~74) has no `maxlength="500"` even though `validate()` (line 26) caps it there; and the date-of-birth input (`FIELDS`, line 11) has no `max` attribute, so the native picker lets users pick a future date that then gets rejected on submit. Both are small UX misses versus the validation you already wrote.
12. A few hard-coded colors bypass your CSS custom properties (`#e0e8f9` hover tint at `css/app.css:110`, `#fdecea` error background at `:178`) instead of deriving from `--brand`/`--none`. Not urgent, but if the palette shifts later these will drift silently.

I'd treat 1–4 as launch blockers, 5–8 as strongly recommended, and 9–12 as quick cleanup. Want me to start fixing any of these?
