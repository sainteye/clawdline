I reviewed `signup.html` plus `signup.js` and `styles.css`. Biggest issues, roughly in order of severity:

**Blocking / accessibility**
- **Submit control isn't a real button** (`signup.html:42`): `<div id="submit" onclick="submitForm()">` is not focusable and has no keydown handler, so keyboard-only and screen-reader users can't submit the form at all. Use `<button type="submit">`.
- **Focus outline removed with no replacement** (`styles.css:7`): `input:focus, textarea:focus { outline: none; }` kills the focus indicator for every field — fails WCAG 2.4.7. Needs a visible focus style if `outline` is removed.
- **Email/phone fields have no `<label>`** (`signup.html:21,24`) — only `placeholder`. Placeholders disappear on input and aren't a reliable label for assistive tech. Add real (can be visually-hidden) `<label>`s like the `notes` field already does.
- **`<html>` has no `lang` attribute** (`signup.html:2`).
- **Progress image has no `alt`** (`signup.html:12`): `steps-1-of-3.png` needs `alt="Step 1 of 3"` (the divider image correctly uses `alt=""` since it's decorative).

**Functional gaps**
- **JS validation misses the `name` field** (`signup.js:1-13`): `submitForm()` only checks email format and the terms checkbox — an empty "Full name" will submit fine even though it's marked `required`, because the form has `novalidate` and nothing else enforces it.
- **No feedback when the terms checkbox is unchecked** — `submitForm()` just silently returns; the user gets no indication why nothing happened.
- **Error message isn't announced to screen readers** (`signup.html:22`): toggling `hidden` with no `aria-live` region and no `aria-invalid`/`aria-describedby` link on the `email` input means sighted users see the red text but screen-reader users get no signal.
- **`<form>` has no `action`/`method`** (`signup.html:15`), so `form.submit()` just reloads the current page — likely a placeholder, but worth confirming before this goes live.

**Minor**
- Heading levels jump from `h1` to `h4` (`signup.html:16,26`) with nothing in between — should probably be `h2`.
- `.help` text color `#b3b3b3` on white (`styles.css:8`) is very low contrast (~2:1), fails WCAG AA for body text.

Want me to go ahead and fix these in the file?
