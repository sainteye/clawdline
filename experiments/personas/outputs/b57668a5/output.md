I read `signup.html`, `styles.css`, and `signup.js` (no live browser/AT tool is available in this sandbox, so I couldn't run an automated scanner or an actual keyboard/VoiceOver/NVDA pass — the findings below come from tracing tab order, accessible-name computation, and live-region/contrast math directly against the shipped markup and CSS. I'd still want a real keyboard + screen-reader pass before shipping; flagging that gap rather than claiming I did one).

## Findings

**1. Submit control is not keyboard operable — blocks the entire flow**
`signup.html:42` — `<div class="button" id="submit" onclick="submitForm()">Register</div>`. A `div` is not in the tab order and has no button role, so keyboard users can't reach it and screen readers announce it as plain text, not a button. This fails **2.1.1 Keyboard (A)** and **4.1.2 Name, Role, Value (A)** — nobody using a keyboard or screen reader can submit the form at all.
Fix: `<button type="submit">Register</button>`, and move the validation from `onclick` to a `submit` listener on `#signup` in `signup.js`. Confirm: Tab to the control, see it's announced "Register, button," and press Enter/Space to submit.

**2. No error feedback for the terms checkbox or the name field**
`signup.js:1-13` only validates email format and whether terms is checked; if either fails it just `return`s with no announcement. The terms checkbox (`signup.html:35`) has no error element at all, and `name` (`signup.html:19`) is `required` but never checked in JS. Since the form has `novalidate`, native browser validation is disabled and nothing replaces it. Fails **3.3.1 Error Identification (A)**.
Fix: add a visible+associated error message for terms (same pattern as email), and validate name in `submitForm`/submit handler.

**3. Validation error isn't announced to screen readers**
`signup.html:22` — `#email-error` toggles `hidden` off on failure but has no `aria-live` region and isn't wired to the input via `aria-describedby`, so a screen reader user who doesn't move focus to it never hears it. Fails **4.1.3 Status Messages (AA)**.
Fix: add `aria-live="polite"` (or `role="alert"`) to `#email-error`, add `aria-describedby="email-error"` on `#email`, and toggle `aria-invalid="true/false"` on the input in JS. Do the same for the new terms error.

**4. No visible focus indicator anywhere in the form**
`styles.css:7` — `input:focus, textarea:focus { outline: none; }` with no replacement style, and it applies to every input including the radios and checkbox. Keyboard users can't see where focus is. Fails **2.4.7 Focus Visible (AA)**.
Fix: remove the rule or replace with something visible, e.g. `outline: 2px solid #1d4ed8; outline-offset: 2px;`.

**5. Email and phone fields have no label, only a placeholder**
`signup.html:21` and `signup.html:24` — `placeholder="Email address"` / `placeholder="Phone (optional)"` with no `<label>`, unlike the `name` field which does this correctly. Placeholder text disappears once the user types and isn't a reliable accessible name in every AT/browser combo. Fails **3.3.2 Labels or Instructions (A)** and **1.3.1 Info and Relationships (A)**.
Fix: add `<label for="email">Email address</label>` and `<label for="phone">Phone (optional)</label>` the same way `name` is done, and move the current placeholder text to a hint if you still want it.

**6. Progress image has no alt text**
`signup.html:12` — `<img src="images/steps-1-of-3.png" width="240" height="24">` has no `alt` attribute at all (unlike the divider image on line 33, which correctly uses `alt=""`). This image conveys real information ("step 1 of 3"). Fails **1.1.1 Non-text Content (A)**.
Fix: `alt="Step 1 of 3: Your details"`.

**7. Heading level skips from h1 to h4**
`signup.html:16` and `:26` — `<h4>Your details</h4>` / `<h4>Session</h4>` directly under an `<h1>`. Screen reader users navigating by heading level will hit an unexplained jump and may assume missing content. Falls under **1.3.1 Info and Relationships (A)**.
Fix: use `<h2>` for both section headings.

**8. Low-contrast helper text**
`styles.css:8` — `.help { color: #b3b3b3; }` on white background is ≈2.1:1 contrast, well under the 4.5:1 minimum. This is the text that tells users which fields are required ("Fields without 'optional' are required" at `signup.html:40`), so it's not decorative. Fails **1.4.3 Contrast (Minimum) (AA)**.
Fix: darken to at least `#767676` (matches the existing input border, ≈4.5:1) or darker.

**Minor/note:** `signup.html:37-38` — the notes field's real label is `visually-hidden` and the only visible prompt is the placeholder, which vanishes once typing starts. Not a strict SC failure (a label is programmatically associated), but worth a visible label for sighted users too.

## Already working, keep it
- `name` field: proper `<label for>` + `autocomplete="name"` + `required` (`signup.html:18-19`).
- Session choice: correct `<fieldset><legend>` radio-group pattern (`signup.html:27-31`).
- `autocomplete` on name/email/phone — satisfies 1.3.5.
- Divider image `alt=""` — correctly marked decorative (`signup.html:33`).
- Error text color `#b00020` and the blue submit background both pass contrast on their own (~7.3:1 and ~5:1) — just needs the div-to-button fix above.
- Tab order matches visual/DOM order; no stray `tabindex` traps.

## Conformance today
This does not meet WCAG 2.1 Level A as shipped — the submit control isn't operable by keyboard at all, which alone blocks the whole flow for keyboard and screen-reader users, plus the missing image alt and unlabeled fields are additional Level A failures. AA adds the focus-visibility and contrast issues on top. I'd fix #1–#3 first since those block task completion; the rest are real but don't stop someone from finishing the form once #1 is fixed.
