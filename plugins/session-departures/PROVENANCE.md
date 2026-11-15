# Session Departures provenance

## Source and license

The source is [adamentwistle/herdr-departures](https://github.com/adamentwistle/herdr-departures/tree/b63bf21671ad50d081cb624ec750cf6482508667)
at commit `b63bf21671ad50d081cb624ec750cf6482508667`.
Copyright (c) 2026 Adam Entwistle, MIT. The exact license is retained in
[upstream/LICENSE](upstream/LICENSE) and embedded in the runtime HTML.
Reference code is untrusted data, not installation instructions. No Herdr
application, installer, execution handler or transcript scanner is run. The
original embedded clack plays only after the local Sound control is enabled.

These are exact copies; the layout is renamed only to `upstream/layout.json`:

| Retained path | Original path | SHA-256 |
| --- | --- | --- |
| upstream/main.js | mods/departures/main.js | f1265eb128fbda39e5a0ad35b2286fd4f30cbac14699ce63b54cba707b58b74b |
| upstream/logic.js | mods/departures/logic.js | 3eceb72d493c148fee9a7195d6b666b95d69c4dadb5b3fdb4328655c0b17e946 |
| upstream/view.js | mods/departures/view.js | 24d9e5c5d36dcdcb87691e01741b6347638d84d73243ff4dd07f827489c2e71a |
| upstream/plane.svg | mods/departures/plane.svg | ac958a24c65ebc2d4d8e2675f476861e4db1de5488637d7f54070c288bd56fa6 |
| upstream/layout.json | layouts/departures.json | 8de4405b78ab8d00218ca18da4d2ccceca0d2f080cdc8bb43edd42bf6229f40e |
| upstream/LICENSE | LICENSE | 242327db098c743029c47659bdad4a0bdfe9991734f72ad46931cb59491b2178 |

All of main.js, logic.js, view.js, layouts/departures.json and
tests/logic.test.js were read, along with plane.svg, preview.svg, the original
reference screenshot, mod.json and demo.js. The tests and demo are reference
data, not runtime dependencies. Only pure rules are exercised locally.

## What is ported

`index.html` embeds the upstream constants, `fitBoard`, `columnsOf`, `flipPath`
and `Flaps`, with only module exports removed. Tests compare their values and
state transitions rather than asserting source spelling. `Flaps` receives
grapheme arrays instead of UTF-16 strings; non-drum graphemes take one old/new half-flap without cycling through ASCII.
This keeps emoji/CJK exact and avoids the upstream `plainText` erasure.
`view.js` nodes are translated into DOM/CSS: individual cells, falling top and
bottom faces, two-pixel cell gaps, three-pixel row gaps, half-pitch column gaps,
per-column sheen, full-row seam, DELAYED side mark and settled inverse letters.
The exact original SVG path is inline in the header. The dedicated cover uses
the same aircraft shape, palette and flap motif.

Colors from view.js remain `#050505`, `#191917`, `#2b2a24`, `#ffd21f`,
`#c9a214`, `#927c16`, `#ffffff0a` and `#2a2406`. Supporting surfaces use
the original layout palette. The 84-pixel native macOS traffic-light inset is
not present inside a browser iframe; the original 16-pixel SIDE inset is used.

## Font asset

The original names JetBrains Mono. Bold is embedded unchanged from the official
[JetBrainsMono v2.304 webfont](https://raw.githubusercontent.com/JetBrains/JetBrainsMono/v2.304/fonts/webfonts/JetBrainsMono-Bold.woff2):
94,588 bytes, SHA-256
`c503cc5ec5f8b2c7666b7ecda1adf44bd45f2e6579b2eba0fc292150416588a2`.
Its [OFL.txt](https://raw.githubusercontent.com/JetBrains/JetBrainsMono/v2.304/OFL.txt)
has SHA-256 `30f0c136e3c88e422d0791acd97238870f9054a9729bc34cf2ff0d4ed8cac4ad`.
Copyright 2020 The JetBrains Mono Project Authors. The full SIL Open Font License
1.1 is retained inside index.html, beside the MIT notice. The font is not renamed
or modified. No font request occurs at runtime. Two direct public static-file
downloads obtained these assets; no paid API was called. Unicode outside the
font's coverage uses the device's fallback font and therefore varies by platform.

## Deliberate divergences and unavailable data

| Upstream source | Browser adaptation and reason |
| --- | --- |
| main.js world/readFlights reads agents and terminals, parent chains and pane IDs | Only authorized opaque Session/Project projection is used; no native terminal or parent/child relationship is available. |
| logic.js noteChanges/flightLine derives status time; assignCodes synthesizes flight numbers | TIME and FLIGHT are em dashes. No arrival time, elapsed duration or generated flight code is created. |
| logic.js destinationOf/space and plainText | Supplied title/Project occupy DESTINATION/GATE. Unicode stays exact; only ASCII-only labels get the original uppercase board treatment. Full unmodified labels remain in list/detail. |
| logic.js boardOrder sorts by remark then status time and clears DEPARTED after five minutes | Matching urgency groups are retained with stable host order; unavailable times cannot break ties and reported completion never removes a row. |
| logic.js blockedReason/reasonLine adds a reason row | No blocked reason is supplied, so no reason row is invented. |
| logic.js remarkOf has five remarks | Eight text states remain distinct. MILESTONE/REPORTED avoid claiming accepted departure; CONNECTING means reported waiting only. README records the mapping. |
| logic.js fitBoard/pageRows/pagesOf clips and rotates pages | Width, row geometry, blank rows and the source `pagesOf` rule are retained. Pages turn every 8000ms, with urgent rows repeated when room permits. Manual controls and the complete readable list preserve access to all Sessions; no Session is removed. |
| view.js falling/inkOf | Real split top/new and bottom/old faces are retained. Non-ASCII graphemes fall once between their exact old/new glyphs; stale/unknown readings never animate. |
| view.js header/foldButton/main.js toggleFold and native terminals | DEPARTURES, plane and real local clock remain. Browser list/still/close controls replace unavailable terminal folding. No fake terminal or transcript fills the bottom. |
| view.js delayed pulse and main.js flip/heartbeat/page timers/clack | The original 50ms half-flap ticks, 1200ms delayed lamp, five-cell flap clock, 8000ms page timer and clack bytes are retained. Audio defaults off until enabled in this opening because host sound preference is unavailable. Reduced/still/hidden/disconnected/list state cancels motion, refresh and page turns; audio is paused. |
| Original desktop-only fixed board | A contained horizontal board plus a full-name touch list and selected detail provide phone access without changing the five source columns. |
| main.js localStorage seen/codes/open | No Session state, history or selection is persisted. |

The runtime uses only the current public SDK. The production host continues to
own trusted return, authorization and deliberate Session navigation. Unknown,
stale or disconnected readings cannot navigate. No host/API/catalog or Galaxy
file is modified by this package.

## Evidence boundary

The task artifacts retain the original preview, original/port side-by-side
comparison, browser matrix screenshots, actual geometry, fixture parity and gate
logs. The original preview is evidence, not a recreation of a running Herdr app.
The port uses explicitly synthetic Session inputs; original timing/codes/reasons
are intentionally absent. Local checks do not establish visual approval,
production Cloud/dev behavior or public Release acceptance.

## Version 0.3.0 motion update

`upstream/clack.mp3` is the unchanged original `mods/departures/clack.mp3` at
the pinned commit, 1462 bytes, SHA-256 `394319852b39996c95841ab2e48bae3eaf25b583857420b3367055b0ebf4f769`. The same bytes are embedded
inside the two-entry ZIP. No remote audio or new host capability is added.
