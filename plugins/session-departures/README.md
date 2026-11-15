# Session Departures

Source-faithful browser port of [Herdr Departures](https://github.com/adamentwistle/herdr-departures/tree/b63bf21671ad50d081cb624ec750cf6482508667),
MIT, copyright 2026 Adam Entwistle. Package ID
`com.clawdline.session-departures`, version `0.2.0`, View Plugin v1 revision 8.
This corrects the earlier green card interpretation using the original source,
not its recipe alone. See [PROVENANCE.md](PROVENANCE.md) for retained source,
licenses, hashes, adaptations and explicit gaps.

The original DEPARTURES header, plane, five column proportions, sizing formulas,
black/yellow palette, rounded individual cells, column sheen, one-pixel seam,
inverted DELAYED letters and forward half-flap wave are retained. The original
JetBrains Mono bold is embedded under its SIL OFL 1.1 license. CJK and other
non-drum graphemes stay static in the same cells; no transliteration removes
Unicode names. Platform fallback fonts supply glyphs absent from JetBrains Mono.

TIME and FLIGHT show an em dash because this projection has neither Session
timing nor actual flight IDs. GATE carries the supplied Project label. Names and
Projects are shortened visually to the original column capacity; the readable
list and selected detail always contain the exact complete values. The board
scrolls inside its own region at phone width, with an obvious readable-list
button and at least 44-pixel list targets. There is no whole-page horizontal
overflow. The host retains its trusted return control outside the package.

## Honest status and navigation

The upstream urgent/completion/working/connecting/idle ordering is adapted to
the eight reported states. Waiting-for-you rows come first, followed by reported
completion, working, waiting-for-Session, ready/on-hold, and unknown. Original
host order is stable inside each group; no unavailable activity time breaks ties.
No row expires and no pagination hides authorized Sessions.

| Reported state | English board | Taiwan Traditional Chinese board |
| --- | --- | --- |
| ready | AT GATE | 就緒 |
| working | BOARDING | 工作中 |
| holding | ON HOLD | 暫停中 |
| waiting_you | DELAYED | 等待你 |
| waiting_session | CONNECTING | 等待其他工作 |
| unknown | UNKNOWN | 狀態未知 |
| milestone_complete | MILESTONE | 回報階段完成 |
| work_complete | REPORTED | 回報工作完成 |

The list and detail spell out the complete localized reported state. CONNECTING
does not assert a parent/child relationship; REPORTED does not assert acceptance.
No machine, hierarchy, blocked reason, transcript, ETA or elapsed time is invented.
The separate header clock is real local browser time, labelled as such. Its
refresh pauses with hidden/reduced/still/disconnected context.

Selecting a row only selects. Only the explicit Open Session detail button calls
the SDK. Unknown-state, stale, unknown-freshness and disconnected rows cannot
open. Reconnect waits for a new snapshot. Changed fresh ASCII cells use the
original drum; stale and unknown rows do not animate. Still mode, hidden context,
reduced motion and disconnect settle and stop the wave. There is no audio,
external runtime request, Session/history persistence or host modification.
All untrusted labels use text nodes. English and Taiwan Traditional Chinese
follow the host language together.

## Build and verify

Use installed dependencies and the existing local Playwright module:

```sh
node tools/ensure-web-deps.mjs
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-departures
python3 tools/pack-view-plugin.py plugins/session-departures web/console/public/view-plugins/session-departures.zip
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs PLUGIN_ARTIFACTS=/absolute/evidence node plugins/session-departures/acceptance.mjs
tools/check.sh
```

The public packer produces a deterministic two-entry STORE ZIP containing only
manifest.json and index.html. SDK, artwork, font and license text are inline;
the retained upstream references are not extra runtime archive entries.

Acceptance imports the actual ZIP through the production manager, registry,
frame, bridge and SDK with synthetic loopback data. Chromium and WebKit run at
390 and 1440 pixels. Checks include both languages, all eight states, ordering,
full Unicode/list detail, hostile labels, null Project, stale and unknown safety,
snapshot changes, selection, explicit navigation, reconnect, empty readings,
trusted return and stopped motion. Sizing, columns, drum and Flaps are compared
against retained upstream functions on representative fixtures; browser checks
measure actual cell widths/heights, seam position and heading alignment.

`PLUGIN_FIDELITY_MUTATION=cell-width` deliberately changes rendered cell width
inside the isolated acceptance page; the geometry check must reject it. This
does not edit the authored package. Screenshots and geometry JSON accompany
each matrix case. Passing these checks is not artistic approval, native phone
acceptance, production authorization or deployment evidence.
