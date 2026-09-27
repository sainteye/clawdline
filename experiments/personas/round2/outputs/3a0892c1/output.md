I read through every file that ships to the browser (`index.html`, `app.css`, and all of `js/`) and traced each interactive flow by hand — specialty filter, slot picker, confirm-hold dialog, checkout form, hold countdown, toasts, tips carousel, and the SPA router — simulating exactly what Tab/Shift+Tab/Enter/Space/Escape would do and what a screen reader's accessibility tree would expose at each step. One honest caveat on method: this sandbox has no display and no network to pull down a headless browser/AT stack, so I couldn't run a live axe scan or an actual VoiceOver/NVDA pass — everything below comes from static tracing of the shipped markup, computed CSS (I hand-verified the relevant contrast ratios), and the JS event/focus-management logic, not from operating a running page. I'd still want one live keyboard + screen-reader pass before launch to catch anything runtime-only, but the issues below are all confirmable in code and are the kind that a live pass would reproduce immediately.

**Bottom line: this does not conform to WCAG 2.1 Level A as shipped.** Several Level A failures sit directly in the booking path, not in edge cases.

## Blocking issues

**1. Specialty filter is completely unusable by keyboard — 2.1.1 Keyboard (A), 4.1.2 Name/Role/Value (A), 1.3.1 Info & Relationships (A)**
`js/components/specialty-select.js:11-26` builds the whole control out of plain `<div>`/`<li>` elements with only `click` listeners (`:48`, `:50-53`). Divs aren't in the tab order, there's no `role`, `aria-expanded`, `aria-activedescendant`, or keydown handling, and the visible "Specialty" text (`:12`) is a bare `<span>`, not a `<label for>` — so it has no accessible name even if it were focusable.
- *Evidence*: Tab through the filters bar — focus goes from the search box straight to the clinician list, skipping the specialty control entirely.
- *Fix*: Use a native `<select>` with a real `<label for>` (styling can stay custom via `appearance` overrides). If a custom listbox is required, implement the full ARIA APG combobox/listbox pattern (button trigger, `aria-expanded`, `role="listbox"`/`role="option"`, arrow-key navigation, Escape to close, focus return).
- *Confirm*: Tab to the field, open with Enter/Space, move through options with arrows, select with Enter, close with Escape — no mouse.

**2. Time-slot buttons have no visible focus indicator — 2.4.7 Focus Visible (AA)**
`css/app.css:154-156` sets `.slot-list button:focus { outline: none; }` with no accompanying `:focus-visible` rule anywhere in the file (unlike `.btn`, which correctly has one at `:70-72`). Slot buttons aren't `.btn` elements (`js/views/slots.js:31`), so they get none of that styling.
- *Evidence*: Tab onto any time button — no ring, no border change, nothing distinguishes it from its neighbors until you guess and press Enter.
- *Fix*: Add a `.slot-list button:focus-visible` rule mirroring the `.btn` treatment.

**3. Confirm-hold dialog: no focus moved in, no trap, no Escape, no focus restore — 2.1.2/2.4.3 (A), 4.1.2 (A)**
`js/components/confirm-dialog.js:33-48` sets `el.hidden = false` but never calls `.focus()` on anything inside the dialog; `onClick` (`:16-23`) only handles `click`, there's no `keydown` handler for Escape anywhere in the file; and `settle()` (`:5-14`) never returns focus to the slot button that opened it (`js/views/slots.js:110-117` doesn't refocus it either). The dialog has correct `role="dialog"`/`aria-modal="true"`/`aria-labelledby`/`aria-describedby` in `index.html:54`, but none of that markup is backed by real modal behavior — a keyboard user can still Tab out into the header nav or footer while it's "open."
- *Evidence*: this fires on every single booking attempt, since it's the required step between picking a slot and checkout.
- *Fix*: On open, `.focus()` the dialog or its first control; trap Tab/Shift+Tab between the two buttons; add an Escape keydown handler that cancels; on close, return focus to `slotButton`.

**4. Five-minute hold expires with no warning and can silently discard entered data — 2.2.1 Timing Adjustable (A)**
`js/views/checkout.js:94-106` ticks a countdown every second into `#hold-remaining` (not a live region — correctly so, since per-second announcements would be worse) but there's no mechanism to extend the hold and no warning issued at any threshold (e.g. 60s/10s remaining) before `navigate()` yanks the user back to slot selection, discarding whatever they've typed.
- *Fix*: Add an "extend hold" action, and fire a single polite/assertive announcement plus a visible warning at a fixed threshold (e.g. 60s left) instead of only a post-hoc toast after expiry (`:101`).

**5. Health-tips carousel auto-advances forever with no pause control — 2.2.2 Pause, Stop, Hide (A)**
`js/components/tips-carousel.js:23` starts a 5000ms `setInterval` that runs for the life of the session (`js/main.js:87` starts it once, nothing ever stops it) with zero pause/stop/hide affordance. The slide-hiding/`tabIndex` roving logic (`:14-19`) is actually done well — that part should stay — but the missing pause control is a hard Level A failure on its own.
- *Fix*: Add a visible pause/play toggle, and pause automatically on hover/focus within `.tips` at minimum.

## High-severity

**6. Form errors aren't associated with their fields — 3.3.1 Error Identification (A)**
`js/views/checkout.js:44-56` (`showErrors`) adds `.invalid` and an error `<p>` but never gives that `<p>` an `id`, and never sets `aria-describedby`/`aria-invalid` on the `<input>`. A screen-reader user tabbing to (or refocused onto, via `first?.focus()` on `:55`) an invalid field hears only the field's label — not why it's invalid.
- *Fix*: give each error `id="err-${name}"`, set `input.setAttribute('aria-describedby', id)` and `aria-invalid="true"` in `showErrors`, clear both in `clearErrors` (`:39-42`).

**7. Server-side booking failure is silent to screen readers — 4.1.3 Status Messages (AA)**
`#checkout-status` (`js/views/checkout.js:80`) gets `status.textContent = err.message` on submit failure (`:150`) but has no `aria-live`/`role="alert"`. A screen-reader user just hears the button re-enable, with no explanation.
- *Fix*: add `role="alert"` (or `aria-live="assertive"`) to `#checkout-status`.

**8. Skip link doesn't actually move focus — 2.4.1 Bypass Blocks (A)**
`index.html:11` points to `#main`, but `<main id="main">` (`:26`) has no `tabindex="-1"`. In most browsers, activating a fragment link to a non-focusable element scrolls but does not move focus there, so the very next Tab restarts from the top of the page — defeating the skip link's purpose. Note `js/views/confirmation.js:13,29` does this correctly (`tabindex="-1"` + `.focus()`), so the fix pattern already exists in the codebase, just not applied to `<main>`.
- *Fix*: add `tabindex="-1"` to `<main id="main">`.

**9. Route changes never move focus or update the title — 2.4.3 Focus Order (A), 2.4.2 Page Title (A)**
`js/main.js:40-65` (`render()`) swaps the whole view on every hash change but never focuses anything in the new view (confirmation.js is the lone exception that does this right, at `:13,29`). `renderSlots`'s `<h2 id="slots-heading">` and `renderCheckout`'s `<h2 id="checkout-heading">` never receive focus, and `document.title` is never touched anywhere, so it stays "Book an appointment – Linden Street Clinic" through clinician list → slots → checkout → confirmation.
- *Fix*: in `render()`, after mounting, focus the new view's heading (add `tabindex="-1"` where needed) and set `document.title` per route.

## Moderate

**10. Per-clinician availability indicator is color-only — 1.1.1/1.4.1 (A)**
`js/views/clinicians.js:23` renders `<span class="availability availability--${level}"></span>` with no text content and no `aria-label`. The legend (`:42-46`) explains what the colors mean, but each card's dot itself is silent to screen readers and relies purely on hue (green/orange/red) for sighted users.
- *Fix*: add visually-hidden text or `aria-label` per dot (e.g. "Many times this week").

**11. Slot button accessible name is ambiguous out of context — 2.4.6 Headings and Labels (AA)**
`js/views/slots.js:32` gives each button only the time ("9:00 AM"); the day is conveyed solely by the surrounding `<h3>` (`:39`). Screen-reader users browsing via a flat "buttons" list (a very common navigation mode) see multiple identical "9:00 AM" entries with no way to tell days apart.
- *Fix*: add `aria-label="${formatDay(day)} at ${formatTime(slot.start)}"` on each button.

**12. Toasts auto-dismiss in 2s while carrying essential info — 2.2.1 (A)**
`js/components/toast.js:1,21-23` removes the toast after `DEFAULT_DURATION_MS = 2000`, used for real errors like "Someone just took that time" (`js/views/slots.js:124`) and hold expiry (`checkout.js:101`). No way to pause or re-read it.
- *Fix*: extend duration for error-severity toasts and/or let them persist until dismissed.

**13. Slot-loading errors aren't announced — 4.1.3 (AA)**
`js/views/slots.js:92` writes the error into `#slot-days` (`:69`), which has no `aria-live`.
- *Fix*: add `aria-live="polite"` to `#slot-days` or wrap errors in a live region.

**14. Positive `tabindex="1"` breaks natural tab order — 2.4.3 (A)**
`js/views/clinicians.js:38` — `tabindex="1"` on the search input forces it ahead of everything else in tab order regardless of visual position. Should be removed (default order is already correct).

## What's already working (keep it that way)

- Confirm dialog's static markup (`role="dialog"`, `aria-modal`, `aria-labelledby`, `aria-describedby`) is correct scaffolding — it just needs the JS behavior described in #3.
- Decorative SVG chevron correctly hidden (`aria-hidden="true" focusable="false"`, `index.html:61`).
- `.btn:focus-visible` ring plus a `forced-colors` fallback (`css/app.css:70-79`) is a genuinely solid, modern pattern — just needs extending to slot buttons.
- `#results-count` and `#week-label` correctly use `aria-live="polite"` (`clinicians.js:47`, `slots.js:66`).
- Heading hierarchy (h1 → h2 per view) has no skipped levels; escaping (`escapeHtml`) is applied consistently.
- Confirmation page's `tabindex="-1"` + `.focus()` pattern (`confirmation.js:13,29`) is exactly right — just needs replicating elsewhere.
- Text/background contrast is fine everywhere I checked (body text ~14.8:1, muted text ~6.5:1, links ~6.7–10.4:1, error red ~6.6:1); UI borders and status dots clear the 3:1 non-text threshold, though the amber "low availability" dot is a bit tight at ~3.4:1 against white.

Given the number of Level A failures sitting in the core booking path (filter, slot picker, hold dialog, timeout, carousel), I'd treat this as not launch-ready from an accessibility standpoint until at least items 1–5 are fixed, with 6–9 close behind.
