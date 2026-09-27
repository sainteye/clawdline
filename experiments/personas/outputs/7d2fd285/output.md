I reviewed the three files (`signup.html`, `styles.css`, `signup.js`). This was a **static code-level audit** — I traced the DOM/ARIA semantics, computed color contrast mathematically, and reasoned through keyboard and screen-reader behavior from the markup itself. I did not have a live browser + NVDA/VoiceOver session available in this environment, so treat the keyboard/AT findings below as "verify live" items, not device-tested confirmations — I'm flagging that gap rather than claiming a pass I didn't actually run.

## Findings (most severe first)

**1. Submit control is a `<div>`, not a button — keyboard users cannot submit the form at all**
`signup.html:42` — `<div class="button" id="submit" onclick="submitForm()">Register</div>`
A `<div>` is not in the tab order and has no `role="button"`/keydown handler. There's also no real `<button type="submit">` anywhere, so pressing Enter in a field won't submit a multi-field form either. Screen readers announce it as plain text, not a control.
- **Criterion:** 2.1.1 Keyboard (A), 4.1.2 Name, Role, Value (A)
- **Fix:** Replace with `<button type="submit">Register</button>`, and move `submitForm()` to a `form.addEventListener('submit', ...)` with `e.preventDefault()` as needed.
- **Verify:** Tab through the form; the submit control should receive focus and activate on Enter/Space.

**2. Focus indicator removed with no replacement**
`styles.css:7` — `input:focus, textarea:focus { outline: none; }`
Applies to every text input, radio, and checkbox in the form. There is no visible focus style anywhere.
- **Criterion:** 2.4.7 Focus Visible (AA)
- **Fix:** Delete the rule, or replace with a visible custom style, e.g. `input:focus, textarea:focus { outline: 2px solid #1d4ed8; outline-offset: 2px; }`.
- **Verify:** Tab through every control and confirm a visible focus ring at each stop.

**3. Missing name or unchecked "terms" produces zero feedback**
`signup.js:9-11` — validation only checks email format and the terms checkbox; if `name` is blank or terms is unchecked, the function just `return`s silently. Combined with `novalidate` on the form (`signup.html:15`), native browser validation is also suppressed.
- **Criterion:** 3.3.1 Error Identification (A)
- **Fix:** Add explicit error messaging for the name field and the terms checkbox, same pattern as the email error.
- **Verify:** Submit with name blank and/or terms unchecked; a visible, specific error should appear for each.

**4. Email error isn't associated with the field or announced**
`signup.html:21-22`, `signup.js:6-7` — `#email-error` is toggled via `hidden` but never wired to the input, and isn't a live region.
- **Criterion:** 1.3.1 Info and Relationships (A), 4.1.3 Status Messages (AA)
- **Fix:** Add `aria-describedby="email-error"` and `aria-invalid` (toggled by JS) on the `#email` input; add `role="alert"` to `#email-error`.
- **Verify:** With a screen reader, submit an invalid email — the error should be announced without moving focus.

**5. Email and phone fields have no associated `<label>`, only placeholder**
`signup.html:21,24` — placeholder text disappears once typing starts and isn't a substitute for instructions.
- **Criterion:** 3.3.2 Labels or Instructions (A)
- **Fix:** Add `<label for="email">Email address</label>` and `<label for="phone">Phone (optional)</label>`, matching the pattern already used for "Full name".

**6. Progress image has no `alt` at all**
`signup.html:12` — `<img src="images/steps-1-of-3.png" width="240" height="24">` with no `alt` attribute (not even `alt=""`). Screen readers may fall back to reading the filename.
- **Criterion:** 1.1.1 Non-text Content (A)
- **Fix:** `alt="Step 1 of 3: Your details"`.

**7. Help text contrast fails AA**
`styles.css:8` — `.help { color: #b3b3b3; }` on white is ≈2.1:1; AA requires 4.5:1 for this font size.
- **Criterion:** 1.4.3 Contrast (Minimum) (AA)
- **Fix:** Darken to something like `#595959` (≈7:1) or match `.error`'s color.

**8. Heading levels skip (h1 → h4 → h4)**
`signup.html:16,26` — no h2/h3 in between, which breaks heading-level navigation for screen reader users.
- **Criterion:** 1.3.1 Info and Relationships (A)
- **Fix:** Use `<h2>` for "Your details" and "Session".

**9. Divider image has a fixed 400px width with no reflow guard**
`signup.html:33` — no `max-width: 100%` on images; could overflow on narrow viewports/at 400% zoom.
- **Criterion:** 1.4.10 Reflow (AA)
- **Fix:** `img { max-width: 100%; height: auto; }`.

## What already works (preserve these)
- "Full name" input and the "terms" checkbox both have correctly associated labels.
- Session radios are properly grouped with `fieldset`/`legend`.
- `autocomplete` attributes are present on name/email/phone.
- The decorative divider image correctly uses `alt=""`.
- Error text (`#b00020`) and button text-on-blue both pass AA contrast comfortably.
- The notes textarea has a (visually-hidden) associated label.

## Conformance today
This form does not conform to WCAG 2.1 AA. Item #1 alone is a full keyboard blocker on the primary task (a keyboard-only user cannot submit the form at all), and #2 breaks visible navigation for every field. These two should be fixed first; the rest are real but lower severity.

Want me to go ahead and apply these fixes to the three files?
