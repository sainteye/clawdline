# Session Orrery

An independently importable View Plugin v1 candidate: a brass astronomical instrument
for the host's authorized Session summaries. ID `com.clawdline.session-orrery`, version
`0.1.0`, names **Session Orrery** / **Session 星象儀**. This directory does not add a
curated host card, publish a package, or install anything into a production browser.

## Reading the instrument

The central sun represents the person. Distance encodes the supplied work state,
in this order from the center: waiting for you, waiting for another Session,
holding, working, milestone reported, completion reported, ready, unknown.
Every state also has its own readable label and symbol in the attention register
and eyepiece; color is supplementary. Completion and milestone detail explicitly
explain that these are reports, not acceptance or final completion respectively.

Project bearings are a deterministic hash of the opaque Project ref (a separate
bearing is used for null Project). Each Project has a fixed twenty-degree sector
with a brass rim pin and faint dotted boundaries. Session refs determine a
longitude inside that sector. State changes move a point between rings without
reshuffling other identities. Host refs are regenerated on opening: positions are
stable within the current opening, not a promised persistent identity across openings.
Project sectors can overlap. Dense points can also overlap; the complete named,
numbered attention register is the authoritative selectable reading in that case.
No Session is capped or silently dropped. The fixed-size point is a diagram mark;
its button has a 44-pixel hit area. In a dense diagram those hit areas can overlap,
so the register provides separate full-width targets for every Session.

The register includes full titles, Project labels (or No Project), work states and
freshness. Selecting a point or row only changes the eyepiece. Only the explicit
**Open Session / 開啟 Session** button calls `sessions.open`; stale/unknown rows
cannot open. Reported state counts include all rows, including stale rows; the
waiting count beside the Session total includes fresh waiting-for-you rows only. A disconnection
immediately makes all retained readings stale; reconnect waits for a new snapshot.

Readable-list mode preserves the same selection and complete data, while hiding
the diagram. Desktop places the instrument and eyepiece/register side by side;
mobile stacks the instrument, eyepiece and register, then the almanac. The host
retains its trusted return control outside the package.

Only actual work-state changes animate position, and only while fresh, visible
and connected. There is no perpetual rotation, polling, duration or growth timer.
Reduced motion, hidden context, still mode, stale rows and disconnection stop
transitions. Still mode is a visible toggle; it cannot override host reduced motion.
Escape first clears the selected detail and then requests host close.

English and Taiwan Traditional Chinese copy ship together and follow host language.
The deliberately dark instrument palette is retained for both host themes. System
fonts are used; no fonts, artwork, dependencies or content are fetched remotely.
All titles and labels are inserted as text. No machine identities, transcripts,
start times, durations, progress percentages, or dependency edges are available or inferred.

## Build

From the repository root, with repository-prepared web dependencies and an already
installed Playwright module (no dependency installation is part of the package):

```sh
node tools/ensure-web-deps.mjs
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-orrery
python3 tools/pack-view-plugin.py plugins/session-orrery web/console/public/view-plugins/session-orrery.zip
```

The authored `cover.svg` is rendered into the manifest's validated 128-square RGBA8
PNG. The public deterministic Python packer inlines the repository SDK marker.
The resulting ZIP has exactly two uncompressed STORE entries: `manifest.json` and
`index.html`. SVG, README and acceptance source remain outside the archive.

Import the ZIP through **View plugins** and review its fixed read/open permissions.
The package follows the existing browser-local installation model; importing it in
one browser does not install it on another device. Package bytes do not modify the
host, SDK, classifier, capacity register, contract or dispatch system.

## Acceptance

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs PLUGIN_ARTIFACTS=/tmp/session-orrery-evidence node plugins/session-orrery/acceptance.mjs
tools/check.sh
```

`acceptance.mjs` bundles the real production manager, registry, frame, bridge and
SDK; starts an ephemeral loopback-only synthetic source; imports the packaged ZIP
through the real file input; and closes its server and browsers in `finally`.
The synthetic source is visibly labeled. It never connects to a running daemon,
Cloud, the person's Chrome, or real Session content. The fixture does not add the
candidate to `Host.tsx` or change production control positioning.

The compact matrix is Chromium and WebKit at 390 pixels in Traditional Chinese
and 1440 pixels in English. It exercises initial ten-row data, eight-state counts,
hostile long labels, null Project, fresh/stale/unknown, selection without navigation,
readable-list parity, unchanged identity position, snapshot title/state updates,
event-only transitions, reduced motion, still mode, host visibility, disconnect and
reconnect, forty-row density, empty snapshots, explicit navigation, touch control
height and whole-page horizontal overflow. Host visibility is simulated in the
synthetic outer document and passed through the production frame's normal context
path; it is not an actual background-tab operating-system test. Screenshots record
each engine/width/language in instrument, list and dense states, and `results.json`
records passed cases. Screenshot names identify synthetic data; these are not
production screenshots or evidence of live device installation.

The root must review the artwork and delivered bytes before enabling the candidate.
Passing browser checks does not establish visual approval, physical-phone behavior,
public Release acceptance, hostile-code isolation or production Session navigation.

The repository gate validates every authored package and its dedicated cover
independently from the curated catalog. Curated entries have a separate coverage
check; adding this candidate does not add it to the host's recommended list.
A browser pass alone does not replace the required repository commit gate.

## Original artwork and reference

The instrument, sun, ticks, spheres and cover are original local SVG/CSS, without
third-party visual assets. The upstream Orrery recipe was consulted only as a
design reference: <https://github.com/isaacin42/herdr-layouts/blob/main/orrery/RECIPE.md>.
No upstream source, fonts, screenshots or artwork are embedded. The terminal
eyepiece, machine labels, elapsed-time growth and unavailable operations in that
reference are deliberately not represented by fabricated v1 data.
