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
- [The Projects page](#the-projects-page)
- [Three things a browser found and no suite did](#three-things-a-browser-found-and-no-suite-did)
- [Two more, from the page that followed](#two-more-from-the-page-that-followed)
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

### The session id in the fragment is percent-encoded

`#session=<id>` carries the id **encoded**, and the encoding is not decoration. The sessions this
app watches are usually tmux panes, and a pane id is `%141` — `Sources/Tmux.swift` calls `%12`
"stable for the life of the pane". Written raw, the address read `/#session=%141`, and the reader
here answers a fragment with `decodeURIComponent`, which does not refuse that: `%14` is a complete
escape, so the id the page went looking for was U+0014 followed by `1`. No session has ever had
that id, so `byId` found nothing, the first whole list let go of the request, and tapping a
notification stopped on the session list with nothing on screen to say why. iTerm's ids —
`w0t0p0:<UUID>` — have no per-cent in them and survived both roads unchanged, which is why this
only ever happened on the machines running tmux and never against a fixture like `abc`.

`WebPush.sessionURL(forSessionID:)` is the one place the address is written, and all four pushes
that name a session go through it. Its allowed set is the **unreserved** characters of RFC 3986 —
alphanumerics plus `-._~` — rather than `.urlFragmentAllowed`, which permits `&`, `=` and `#`: the
reader is `/(?:^|[#&])session=([^&]*)/`, so any of those three inside an id would cut the fragment
in half. The `tag` beside it in each caller is not a URL and is left alone.

Reading it back takes **two candidates, not one**: the decoding first, and the text exactly as it
was written after it. A notification already delivered to somebody's phone carries the old
spelling and gets tapped days later, so `route.js` holds both across the wait for the first
session list and tries them in that order.

**The second candidate is kept only where the decoding is impossible**, which is what an old link
looks like: `%141` decodes to U+0014 and a `1`, a control character no session id contains. A link
written since the encoding decodes to a real id instead — `%25141` to `%141` — and there the raw
text names a *different real session*: `%252` is how `%2` is spelled now, and on a machine that has
reached pane `%252` the raw reading would open that stranger the day `%2` closes, with nothing on
screen to say so. The cost of the rule is one narrow range — an old link naming `%20`–`%39`
decodes to a printable character and is not rescued — and that is the behaviour this had before
the encoding existed, which is "does not route", never "routes somewhere else".

## Adding a page

Three things, and no new mechanism. The **Projects page** was the first one added this way, and it
cost exactly these three — it sits between `sessions` and `usage`, because it is a way *into* the
sessions rather than a reading about them.

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

A page with a **fourth** thing — a key of its own, a shortcut, a second address — is the point at
which to stop and read the note below on where Escape lives. It is the one thing about this
mechanism that a suite driving a stand-in document will tell you is fine.

## The Projects page

`Resources/web/app/js/view/projects.js`, styled in `app/css/projects.css`, drawn from
`Tests/web-projects.mjs`. It answers two questions and the second one is why it exists.

The first is **where a session could be started** — `/v1/places`, directories an assistant has
actually been run in and that are still on the disk. That is the list.

The second is asked standing in front of one of them: **which of this Project's worktrees finished
a Feature, and did that delivery reach the branch.**
[`GET /v1/orchestrator/usage/project-worktrees`](api.md#get-v1orchestratorusageproject-worktreesprojectidnamepath)
answers it, and it is deliberately not `git worktree list`: this Mac carries 58 managed checkouts
and the ledger remembers 150, most of which produced nothing anybody kept. Only the ones carrying
an accepted Feature head are listed; the rest are counted.

**The screen has one subject and it is `delivered`.** Thirty-eight of this repository's
seventy-nine Feature-carrying worktrees finished their work and have no landing record — the first
number anybody has had for "it gets done and nobody merges it". That figure is not this page's
reading: it was measured on 2026-09-04 by the route's own rules run over a copy of the production
ledger, alongside 35 `landed` and 6 debris, out of 150 worktrees the ledger remembers and 58 still
on the disk. Thirteen more resolved to no Project at all. So it is the open block at the
top, with the branch each one is on. The payload carries no `branch` on purpose (the ledger stores
none and the registry that does is swept), so what is drawn is the convention
`clawdline/task/<worktree id>` under a label that says it is one. The other four rungs are closed
`<details>` underneath, each with the stored fact it rests on rather than a description of itself.

**An empty answer and an answer that never arrived are drawn differently**, which is the half of
the route's work a page throws away most easily. Every answer carries a `read` receipt; it is on
screen whenever the route answered and cleared whenever it did not — including the moment before a
request goes out, so a refusal cannot be left wearing the last Project's numbers.
`project_not_found` and `ambiguous_project` each get their own sentence, because the useful next
move after them is different. `unattributed` is counted in a block that says it belongs to no
Project rather than dropped.

The Project detail is a **view inside the page** rather than an address of its own. `#page=` is the
whole fragment — a page is not a session — so a second key here would be inventing an addressing
scheme beside one that had just been agreed.

Neither read exists on the Cloud path, so `main.js` asks
`typeof api.places === "function" && typeof api.projectWorktrees === "function"` **when the page is
used** rather than when it is bound: `net/api.js` holds a live binding the entry point fills in,
and on the Cloud path it fills it in twice.

## Two more, from the page that followed

Same fifteen seconds of setup, same answer: both were green in Node at the moment they were broken.

**The Project's identity read right-aligned.** `.projects-heading` pushes its two halves apart,
which is what the list's count needs and the opposite of what an identity is — the mark and the
name are one thing. A stand-in document has no layout, so nothing but looking could have caught it.

**Escape closed the drawer and left the page under it, for one press.** The page answered Escape
with a listener of its own and stood down while the drawer was open, which is the right rule and
was never true: `input/keys.js` closes the drawer and *returns*, and **returning is only ever true
of the listener doing it.** By the time a second listener on the same document ran, the drawer it
was checking for was already shut. A harness with one listener has nothing to be second to.

The fix was not a better guard. This page's Escape moved into `input/keys.js` — the one place where
the order of Escape is decided — reached through an exported `Projects.escape` the way
`Settings.close` already is, and placed after the drawer's turn and the shortcuts card's.
`Tests/web-projects.mjs` now refuses a key listener in `view/projects.js` at all and pins that
branch's position, because the mistake is invisible in a diff that touches one file.

`view/usage.js` had the same defect in its own Escape listener. It was found twice
independently — by the review of the slice that added the drawer, and again here — and was
repaired there before this page landed, so the chain in `input/keys.js` is now the only place a
page answers Escape.

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
