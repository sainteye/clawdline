I read through `signup.html` plus `styles.css` and `signup.js` for context. Here's what stands out, worst first:

**Breaks the form entirely for keyboard/AT users**
- `signup.html:42` — the submit control is a `<div onclick="submitForm()">`, not a `<button>`. It's not focusable, can't be activated with Enter/Space, and isn't announced as a button by screen readers. Since there are 3 text-type inputs, HTML's implicit-submit-on-Enter doesn't kick in either — a keyboard-only user has no way to submit this form. Fix: `<button type="submit">Register</button>` and drop the `onclick` (listen for the form's `submit` event in JS instead, and `e.preventDefault()` when invalid).

**Validation silently doesn't work as intended**
- `signup.html:15` has `novalidate`, but `signup.js` only checks email format and the terms checkbox — it never checks `name`. Since `novalidate` disables the browser's native enforcement of `required` on `#name`, a blank name currently submits successfully. Either validate name in JS too, or drop `novalidate` and let the browser enforce `required` alongside your custom email check.

**Missing labels**
- `signup.html:21` (email) and `:24` (phone) have no `<label>`, only `placeholder`. Placeholder text disappears once typing starts and isn't a reliable accessible name for screen readers. Add real `<label>`s (visually-hidden if you don't want them shown, like you already did for `notes`).

**Focus indicator removed with no replacement**
- `styles.css:7` — `input:focus, textarea:focus { outline: none; }` with nothing to replace it. Keyboard users can't see which field is focused. Add a visible focus style (e.g. `box-shadow` or `outline` in an accent color) or just remove this rule.

**Error isn't announced to screen readers**
- `signup.html:22` — `#email-error` is toggled via `hidden`, but it's not wired to the input with `aria-describedby`, there's no `aria-invalid` on the field, and the region isn't `aria-live`, so a screen reader user who submits a bad email gets red text they can't see and no announcement.

**Progress image has no alt text**
- `signup.html:12` — `<img src="images/steps-1-of-3.png">` conveys real information ("step 1 of 3") but has no `alt` attribute at all, so it'll fall back to announcing the filename. Add `alt="Step 1 of 3"`.

**Smaller polish items**
- `signup.html:2` — `<html>` has no `lang="en"`.
- `signup.html:16,26` — heading jumps from `<h1>` straight to `<h4>`, skipping levels; should be `<h2>`.
- `styles.css:8` — `.help { color: #b3b3b3 }` on white is roughly 2.3:1 contrast, well under the 4.5:1 AA minimum — and it's the text explaining which fields are required, so it's easy to miss and hard to read.

Want me to go ahead and make these fixes?
