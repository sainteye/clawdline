# Session Departures

An independently installable View Plugin v1 candidate, ID
`com.clawdline.session-departures`, version `0.1.0`. The package owns its header;
the production host retains its trusted return control. The host offers this package in **View plugins → Explore views**. The normal
package review is required and installation remains local to each browser or
device. Source or Cloud publication does not establish public Release acceptance.

## Presentation and data

The original airport board uses warm yellow signwork, dark green flap rows, an
authored aircraft mark and a dedicated cover. Taiwan Traditional Chinese and
English follow the host language. Names remain Unicode text: there is no Latin
character drum, uppercase conversion, name clipping, generated flight code, or
substitution of identity during motion. Every row includes its complete title,
Project (or an explicit unassigned label), readable state and freshness. At phone
widths, Project moves below the title and the state moves onto its own line.

`waiting_you` rows are pinned first; the host order is preserved within the two
groups. All eight reported work states are individually named. No row is removed
because it reports completion, and no automatic paging hides rows. The selected
detail retains the complete title and Project across updates, reordering and the
optional readable list. Only the explicit detail button calls `sessions.open`.
Stale or unknown-freshness rows cannot open. Disconnection immediately makes all
retained rows stale, and reconnect awaits a new snapshot before enabling them.
Titles and Project labels are inserted through `textContent`, never HTML.

Only a changed, fresh row has a brief flap-light transition. Text never blanks or
cycles through random glyphs. Motion stops for hidden context, reduced motion,
disconnection, stale readings, or the explicit still-mode control. A resting
board has no animation loop, clock, sound, auto-paging, or periodic timer.

The v1 projection supplies no machine identity, actual start time, ETA, transcript,
blocked reason, dependencies or progress percentage. This package invents none.
Completion remains a reported state, with an explicit acceptance caveat. It uses
every Session in the current authorized host snapshot, without private APIs or
external requests. Packages and preferences are browser-local; this view does not
persist Session data or remember a selection after closing.

## Build

From the repository root, use the existing local browser installation to render
the authored SVG into a validated embedded RGBA8 PNG, then run the public packer:

```sh
node tools/ensure-web-deps.mjs
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-departures
python3 tools/pack-view-plugin.py plugins/session-departures web/console/public/view-plugins/session-departures.zip
```

The deterministic archive contains exactly `manifest.json` and `index.html`,
stored without compression. The packer inserts the current repository SDK at
the HTML marker. `cover.svg` is editable source, not an extra archive entry.
Import the ZIP through the host's file input and review its identity and access.
No host source or catalog change is required.

## Acceptance

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs PLUGIN_ARTIFACTS=/absolute/evidence/directory node plugins/session-departures/acceptance.mjs
tools/check.sh
```

`acceptance.mjs` bundles the real production React manager, registry, frame and
bridge with esbuild and imports this ZIP through the production file input. Its
own loopback-only server and isolated browser processes close after the run.
All Session data is explicitly synthetic. No daemon, real Cloud account, usual
Chrome profile or production Session is used. No new dependency or paid API is
needed; use the repository dependency preparation command rather than installing
into shared dependencies manually.

The compact matrix covers Chromium and WebKit at 390 and 1440 pixels, changing
between English and Traditional Chinese inside each run. It checks all eight
states, initial snapshot and update, waiting-first order, full Unicode detail,
hostile text, null Project, stale/unknown freshness, list/selection parity,
deliberate navigation, active and stopped motion, disconnect/reconnect, empty
data, horizontal overflow and the trusted return control. Each case saves board,
detail and empty screenshots; `results.json` lists completed cases. The hidden
context case changes the synthetic host's visibility property and emits the
normal visibility event through the production frame. It is a deterministic
context test, not an operating-system background-tab measurement.

Browser layout acceptance does not establish native iPhone behavior, real fleet
authorization, Cloud deployment, public publication, or artistic approval.

The repository gate validates every authored package and its dedicated cover
independently from the curated catalog. Curated entries have a separate coverage
check; adding this candidate does not add it to the host's recommended list.
A browser pass alone does not replace the required repository commit gate.

## Reference

The airport departures metaphor was informed by the public
[Herdr Departures recipe](https://raw.githubusercontent.com/adamentwistle/herdr-departures/main/RECIPE.md).
Its source, artwork, sound and Latin-only drum are not incorporated. This view's
HTML, SVG, layout, copy and interaction logic are original local work adapted to
the narrower Clawdline v1 data projection.
