# Which page the web app is on

Until 2026-09-04 the web app was one page with things laid over it. The session list was the
document; Usage was a fixed panel that hid `#app` from inside its own module; Settings was a modal
sheet behind the wordmark. Neither could be linked to, neither was anywhere in particular, and
every new destination had to invent its own way of appearing and its own way of putting the page
back.

There is a place to *be* now. This page is how it works and what adding the next one costs.

- [What a page is](#what-a-page-is)
- [The drawer](#the-drawer)
- [The address](#the-address)
- [Adding a page](#adding-a-page)
- [Three things a browser found and no suite did](#three-things-a-browser-found-and-no-suite-did)
- [What deliberately did not change](#what-deliberately-did-not-change)

## What a page is

A page is a top-level section of `Resources/web/index.html` that fills the document below the
header. Exactly one is on screen; the rest carry `hidden`.

`hidden` is the mechanism on purpose, because it is the one both of the surfaces being moved were
already using — `input/keys.js` still asks `els.settings.hidden` whether the settings page is on
screen and gets the same answer it always did, and Usage's own Escape still guards on
`elements["usage-analytics"].hidden`. Nothing had to learn a new way of asking.

`Resources/web/app/js/core/pages.js` is the whole of the deciding:

| | |
| --- | --- |
| `Pages.bind({document, root, pages, onChange, writeHash})` | the registry, given once by `main.js` |
| `Pages.go(name, {hash})` | move there; idempotent, and refuses a name it does not know |
| `Pages.goHome()` | the first page in the registry — what every Close and Escape means |
| `Pages.current()` / `Pages.home()` / `Pages.knows(name)` | readings |

It knows no page names of its own. The array `main.js` hands to `bind` is the registry, in the
order the drawer names them, **home first** — that ordering is not decoration, it is how `goHome`
decides where home is.

Each entry may carry `enter`, `leave` and `focus`:

- **`enter` runs on arrival however the arrival happened** — a row in the drawer, a pasted
  `#page=…`, the browser's Back. That is why the Usage page fetches its portfolio in `enter`
  rather than in a click handler, and why `Settings.enter` draws the version, the notification
  block and the two toggles rather than `Settings.open` doing it. `open` is now one line: go there.
- **`leave`** is the seam for the page being left. Both of this slice's pages have nothing to put
  back; the seam exists because a page that does will not want to invent one.
- **`focus`** is the id the keyboard lands on once the page is showing, focused with
  `preventScroll` — a page that arrives already scrolled past its own heading is worse than one
  that arrives with no focus at all.

`core/pages.js` **touches no document until `bind` is called**, and `go` before `bind` is a no-op
rather than a throw. Several Node suites import modules that reach it, and a module that reads
`document` while it is being evaluated is a module that cannot be imported without a browser.

Nothing else in the app hides a page by hand. `view/usage.js` used to set `elements.app.hidden`,
`elements.settings.hidden` and the body's overflow itself; `Tests/web-pages.mjs` asserts it no
longer names any of them, because the way that decision comes back is one module quietly deciding
for itself again.

## The drawer

`Resources/web/app/js/input/sidebar.js`, styled in `Resources/web/app/css/pages.css`.

The wordmark opens it. It used to open the settings sheet directly, because settings was the only
app-wide destination there was; it now opens the list of them, and settings is one row of that
list. Same gesture, same corner, one more row every time a page is added — which is the argument
against a second permanent control in a header that has one line on a phone.

It is a drawer at every width rather than a column pinned open on a desk. A phone is the screen
this app is mostly read on, one behaviour is cheaper to keep right than two, and the desk pays a
tap for the session list's full width. It starts below the header so the wordmark stays pressable
while it is open, because pressing it again is the shortest way out and the first thing anybody
tries. It never covers the whole width: the strip of page still showing is what says this is a
drawer over something, and it is what a thumb reaches for to dismiss it.

**Every control that names a page carries `data-page-to="<name>"` and nothing else.** The drawer's
rows, the Usage page's "Back to sessions", the settings page's Close: one delegated listener in
`core/pages.js` answers all of them, walking up from the click target so that a tap on a label
inside a button counts. That is what makes the next page's link a line of markup instead of a line
of JavaScript.

The attribute is `data-page-to` rather than `data-page` because the root element wears `data-page`
as the answer to *which page is this*, and a click that walked up to the root would otherwise read
as a request to navigate.

Which row is lit is `aria-current="page"`, set by `markSidebarPage` from `Pages`' `onChange` —
told by the router rather than by the row that was pressed, because a page can also be reached
from the address bar, and the drawer has to agree with where the app actually is.

## The address

`#page=<name>`, read by `input/route.js` alongside the `#session=<id>` it already read. A name this
build has no page for is ignored rather than obeyed: an old link leaves you where you were instead
of in front of a blank rectangle.

Written back with `history.replaceState`, not `pushState` and not by assigning to `location.hash`.
A page is where you are rather than a step you took — a reload lands back on it, and the Back
button still means the screen before this app rather than three drawer presses ago. It also keeps
this off the one history entry that already means something: `session/open.js` pushes a state on a
phone so the back gesture closes the transcript, and `input/action-confirm.js` pops it.

Opening a session goes home first. `#session=…` means the session list with that session open on
it, and without that line a push arriving while somebody is reading Usage would load the
transcript underneath a page that is still on screen.

## Adding a page

Three things, and no new mechanism. The **Projects page** is the next one, and it belongs between
`sessions` and `usage`: it is a way *into* the sessions rather than a reading about them.

1. **A section in `Resources/web/index.html`**, beside the others:
   `<section class="page" id="projects" data-page-view="projects" hidden>`. `.page` in
   `app/css/pages.css` gives it the area below the header, its own scroll, and
   `display: none` when hidden — written out rather than left to the browser's own `[hidden]`
   rule, which is the weakest thing in the cascade.
2. **A row in the drawer**: `<button class="sidebar-item" id="nav-projects" type="button"
   data-page-to="projects">Projects</button>`. There is a comment in the markup marking the spot.
3. **A line in the registry in `main.js`**, in the same order as the drawer, carrying `enter` if
   arriving means loading something and `focus` if the page has an obvious first control.

`Tests/web-pages.mjs` holds those three against each other: a row naming a page nobody registered,
or a section the router does not know, is red. Neither breaks anything at load time, and neither is
visible in a diff that touches two files — which is why it is checked rather than remembered.

If the new page has words of its own, note what this slice ran into: a **new visible string means a
new member on the `Copy` protocol in `Sources/Strings.swift`, and every conformance has to define
it** — `Copy+English.swift`, `Copy+Chinese.swift` and its simplified sibling. `tools/check-web-strings.py`
holds `T.<name>` in the modules, the fallback in `core/i18n.js` and the `/v1/strings` payload in
`Sources/RemotePage.swift` to each other, so a string added to two of the three is red before a
compiler starts. The drawer's own labels reuse strings that already existed — `webBack` is
"Sessions", `webSettings` is "Settings" — and the wordmark's `Menu` is written in the markup in
English, which is what the untranslated half of the Usage page does too.

## Three things a browser found and no suite did

All three were green in Node, in a fake document, at the moment they were broken. The page was
served with `python3 -m http.server` from `Resources/web` and opened at `?mock=1`; that is fifteen
seconds of setup and it is the difference between a suite that agrees with itself and a page that
works.

**The drawer swallowed its own clicks.** Every sheet in this app closes on a tap outside by putting
`stopPropagation` on the sheet, so the drawer was built the same way — and every page link in it
is answered by *one delegated listener on the document*, which that swallows too. Pressing Settings
lit the row and did nothing, with nothing in the console. It tells a tap on the scrim from a tap on
a row by comparing `ev.target` now, and `Tests/web-pages.mjs` refuses a `stopPropagation` in that
file (with the pattern calibrated against `input/command.js`, which really does have one).

**The first list took a deep link away.** Opening `#page=usage` in a fresh tab came up on Usage and
then, about a second later, went back to the session list and rewrote the address on the way —
`view/list.js` opens the top session when the first list arrives on a desk, and opening a session
means being on the sessions page. It now leaves that courtesy alone unless the app is actually on
the home page. Measured both ways on two ports, because Chrome's module cache will happily serve
the old `list.js` under an unchanged URL and tell you your fix did nothing.

**The drawer did not slide in, in the end.** A `translateX` entrance sits at its first keyframe for
as long as the animation is `running`, and a renderer that throttles animations — measured here in
an iframe Chrome had decided not to animate — leaves the drawer open, `hidden === false`, and
entirely off the left edge with its scrim over the page. `responsive.css` reached the same
conclusion about the detail pane for a different reason two months earlier. The scrim keeps its
`fade`, because an undimmed page is a look rather than a trap.

## What deliberately did not change

This slice moved Usage and Settings; it did not redesign them. Every existing web suite reports the
same numbers it reported before it, `Tests/web-usage-analytics.mjs`'s 127 assertions included.

Two things went with the overlay, because they were the overlay rather than the surface:

- **Tapping outside no longer closes Settings.** A page has no outside. Escape and Close both still
  lead back to the list, and `role="dialog"`/`aria-modal` are gone with the scrim they described.
- **The wordmark's accessible name is `Menu` rather than `Settings`,** and it is in the markup
  rather than painted from the strings — see the note above.

One thing arrived: **the drawer joins the Escape chain in `input/keys.js`, before the settings
page**, and stands the list shortcuts down while it is open. Closing both the drawer and the page
under it for one press would take somebody off Settings when all they wanted was the drawer shut.
