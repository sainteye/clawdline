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
already using — `input/keys.js` still asks `els.settings.hidden` whether a sheet is over the list
before it lets a list shortcut through, and gets the same answer it always did. Nothing had to
learn a new way of asking.

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

**Leaving needs an answer too, and it is not the page's.** `hidden` takes the focused control out
of the document and the browser drops focus on `<body>`, from where nothing can be given back — so
a page that names no `focus` of its own would strand the keyboard every time it was arrived at.
The session list is exactly that page. `bind` takes a **`focusFallback`** id for it (`main.js`
passes `brand`, the wordmark: the one control on screen whatever page this is), and it is used only
on a move away from another page, because taking the keyboard on the first paint is a page
announcing itself to somebody who has not asked it anything.

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

**A fragment that names no page means the home page.** That is the return half, and it was missing
for one commit: `routeTo` acted only when the fragment named a page, so clearing the fragment left
`data-page="usage"` on the root with Usage drawn, `#app` hidden and nothing in the address saying
so — one screen with no address, and a reload from there landing on the session list instead. On a
phone that is what the back gesture does: `session/open.js` pushes one entry, the pop closes the
session underneath it, and the hashchange that follows used to change nothing at all. It goes home
with `{hash: false}`, because an address being obeyed must not be written back to itself.

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
   arriving means loading something and `focus` if the page has an obvious first control. Leaving
   is already answered by `focusFallback`; a page with no obvious first control needs no `focus`
   and will not strand the keyboard for want of one.

Nothing else. In particular **not a line in `input/keys.js`**: Escape asks `Pages.current() !==
Pages.home()` rather than naming pages, so a new page leaves it through the same branch every other
one does.

`Tests/web-pages.mjs` holds those three against each other: a row naming a page nobody registered,
or a section the router does not know, is red. Neither breaks anything at load time, and neither is
visible in a diff that touches two files — which is why it is checked rather than remembered.

If the new page has words of its own: a **new visible string means a new member on the `Copy`
protocol in `Sources/Strings.swift`, and all fourteen conformances have to define it** — the
thirteen `Copy+*.swift` files, with `Copy+Chinese.swift` carrying two. `tools/check-web-strings.py`
holds `T.<name>` in the modules, the fallback in `core/i18n.js` and the `/v1/strings` payload in
`Sources/RemotePage.swift` to each other, so a string added to two of the three is red before a
compiler starts.

**What that guard cannot see is a word written straight into `index.html`**, and it is not a gap it
could close: it reads JavaScript, and an English literal in the markup looks to it exactly like the
English fallback that is supposed to be there. This slice shipped four that way for one commit —
`Menu` on the wordmark, `Pages` on the drawer, `Usage` on its row, and `webBack` reused for
`Sessions` — and a Chinese reader's only navigation read 清單 / Usage / 設定 while the wordmark's
accessible name went from 設定 to an untranslated `Menu`. The English stays in the markup as the
fallback, as it does everywhere in this document; what closes the loop is that `view/static.js`
paints each of them, and `Tests/web-pages.mjs` holds every id in the drawer against its paint.

`webSessions` is a separate string from `webBack` on purpose. They are one word in English and two
elsewhere: `webBack` is the chevron above a transcript — 「清單」in Chinese, with `webBackLabel`
reading 「回到 session 清單」beside it — and a row in a navigation drawer is not that sentence.

## Three things a browser found and no suite did

All three were green in Node, in a fake document, at the moment they were broken. The page was
served with `python3 -m http.server` from `Resources/web` and opened at `?mock=1`; that is fifteen
seconds of setup and it is the difference between a suite that agrees with itself and a page that
works.

**And for one commit, two of the three could be undone in silence.** Each was re-broken on a copy
of this tree — the deep-link condition deleted, the panel's `translateX` put back, the drawer and
the page swapped in the Escape chain — and all 25 web suites and both Python guards stayed green
for every one of them. A fix nothing can go red for is a fix that comes back, so each now has a
guard in `Tests/web-pages.mjs`. None of the three can be held by driving the page in Node: there is
no layout and no animation clock for the CSS, and `input/keys.js` and `view/list.js` both bind
listeners at module scope and pull the whole app graph in behind them. They are held against the
source instead, in the shape the `stopPropagation` check already had — **each pattern calibrated
against a known positive first**, because a zero from a pattern nobody has seen match is a
statement about the pattern rather than about the file.

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
conclusion about the detail pane for a different reason two months earlier.

**Neither does the scrim fade, and this page said the opposite for one commit.** The reason given
was that an animation which never runs leaves an undimmed page behind an otherwise complete drawer
— a look rather than a trap. It was wrong by one fact: `.sidebar-panel` is a child of `.sidebar`,
and `opacity` applies to the subtree. The same throttled renderer therefore did not leave the page
undimmed; it left the whole drawer invisible, and an element at `opacity: 0` still answers a hit
test — so what sat over the page was a fixed layer covering everything below the header and eating
the first click, with the same symptom as the swallowed clicks fixed the day before. That is one
worse than the failure the `translateX` was removed for, in the same file, under a paragraph
arguing for it. `sheets.css`'s `.overlay` is the same construction and is older than all of this;
it is what the guard calibrates against rather than something this slice changed.

## What deliberately did not change

This slice moved Usage and Settings; it did not redesign them. Every existing web suite reports the
same numbers it reported before it, `Tests/web-usage-analytics.mjs`'s 127 assertions included.

Two things went with the overlay, because they were the overlay rather than the surface:

- **Tapping outside no longer closes Settings.** A page has no outside. Escape and Close both still
  lead back to the list, and `role="dialog"`/`aria-modal` are gone with the scrim they described.
- **The wordmark's accessible name is `Menu` rather than `Settings`,** which is what it opens now.
  It is painted from `T.webMenu` like every other label here — see the note above.

One thing arrived: **there is one Escape chain, in `input/keys.js`, and leaving a page is one line
of it.** The drawer is offered the press before any page, because closing both for one press would
take somebody off Settings when all they wanted was the drawer shut; the shortcuts card is offered
it before that, for the same reason one layer up. Then `Pages.current() !== Pages.home()` goes
home, once, for every page there is — unless a `dialog[open]` is over it, which answers its own
Escape.

That last branch replaced a second `keydown` listener inside `view/usage.js`. **A `return` ends its
own listener and nothing else**, so with the drawer open over Usage one press closed the drawer
*and* left the page, while the same three steps over Settings were right — an edge that had not
been connected rather than an order that was wrong.
