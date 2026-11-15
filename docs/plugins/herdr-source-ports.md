# Herdr source ports

The first Session view candidates used recipes and preview images to create
independent artwork. They did not preserve enough of the upstream implementation
to establish visual fidelity. The correction uses pinned rendering source,
geometry and licensed assets, with explicit platform and data adaptations.

| View | Pinned source | Rendering boundary |
| --- | --- | --- |
| Orrery | [isaacin42/herdr-layouts, 73bf616](https://github.com/isaacin42/herdr-layouts/tree/73bf616382d6b5c0ae5f73edf3cd122c4f51cda9/orrery) | `main.js` divides the entire dial into equal space sectors and places planets inside their sector. Preserve that geometry, the original SVG dial/planets/halos, typography and brass/walnut composition. |
| Night City | [vib-z/herdr-layouts, e4619fa](https://github.com/vib-z/herdr-layouts/tree/e4619fad4d5dea423fe1ebf2c296de7e6964f52d/night-city) | `model.js` derives floors, windows and skyline geometry; `main.js` composes the tower, signs, glass and beams. Reuse the composition and assets without inventing missing native hierarchy or history. |
| Departures | [adamentwistle/herdr-departures, b63bf21](https://github.com/adamentwistle/herdr-departures/tree/b63bf21671ad50d081cb624ec750cf6482508667) | `logic.js` and `view.js` own board sizing, columns and split-flap cells. Port those mechanisms while keeping full Unicode names and labeling unavailable fields. |
| Territory | [adamentwistle/herdr-territory, b53947d](https://github.com/adamentwistle/herdr-territory/tree/b53947d6b9ebe10208d1b7edf2948d3e68dd3086) | `treemap.js`, `activity.js`, `frame.js` and `view.js` draw actual files, activity heat, trails and agent positions. Equal-area Project/Session cells are a different visualization and cannot substitute for this data. |

The upstream views use native GPUI/Herdr modules. A browser port must translate
those rendering primitives rather than execute native settings, terminal or shell
actions. Clawdline View Plugin v1 currently supplies Session summaries and
navigation, not a live terminal, file inventory, tool-event history or work timers.
No missing input may be fabricated from Session state. A source-faithful renderer
with synthetic inputs establishes rendering only, not a live Territory feature.

Orrery's repository carries Apache-2.0; its IM Fell fonts carry a separate SIL OFL
notice. The other three repositories carry MIT licenses. Retain the full applicable
licenses, original notices, pinned provenance and a description of modifications
with copied or adapted code/assets. Embedded package copies must also retain the
applicable notices. A public URL is not a substitute for the original license.

Each port must provide an original-versus-port comparison at comparable desktop
size and a separate 390-pixel browser check. Review geometry, assets, palette,
typography, full labels and interaction intent. List unavoidable data/platform
adaptations separately. Runtime safety checks alone do not establish visual
fidelity or physical-phone acceptance.

Installed 0.1.0 packages remain browser-local. A corrected package is a new
version, offered through the host's explicit update review. Publishing a ZIP must
not silently replace a phone's registry or downgrade a newer installed package.

## Motion and composition in 0.3.0

The browser ports follow the same pinned source rather than adding decorative
movement. Orrery has no continuous orbit rotation: state changes retain the
900ms ease-out glide. Its desktop dial now uses the actual iframe height rather
than a 760px minimum, preserving the original center/radius formulas and 1.1:1
composition while the eyepiece scrolls inside its frame. The almanac restores four compact
summary rows; readable-list mode retains all eight state counts. Work-age growth remains
unavailable because the projection supplies no trusted work-start time.

Night City alternates the original `_` / `▏` cursor glyphs at 600ms, retains the
1800ms attention lamp and 3200ms beam, and restores the 300ms neon-sign height
transition. Existing animation phase survives selection and snapshot redraws;
interrupted sign transitions start at their current measured height. Native
multi-tab floors and terminals remain unavailable and are not invented.

Departures restores the original `pagesOf` rule and 8000ms page cadence, with
waiting-for-you rows repeated when space permits. A hovered or focused row does
not move to another page. Previous/Next controls and the complete readable list
retain access to every Session. The local five-cell clock now uses the original
50ms half-flap engine. ASCII follows the original forward drum; other graphemes
fall once between the exact old/new glyphs. The delayed side lamp uses the
original 1200ms opacity pulse. The unchanged MIT-licensed clack is embedded;
Sound defaults off until explicitly enabled in the current opening because the
host does not expose its sound preference. No remote media is requested.

Still, reduced-motion, hidden, disconnected and stale states continue to stop
unsafe or misleading movement. List mode stops the Departures wave and page
timer. Reported completion never removes a Session or implies acceptance. The
Territory file renderer remains a separately labeled synthetic study; these
motion changes grant no file inventory or transcript access.

`plugins/source-motion.acceptance.mjs` measures actual transition durations and
intermediate rendered frames in the production package host, exercises Unicode
old/new faces, a minute-boundary clock flip and source page turns, then checks
still/reduced-motion cancellation in Chromium/WebKit at 390/1440 pixels. A prior
0.2.0 package fails the cursor-glyph check. Existing per-view acceptance retains
freshness, complete-text, authorization and explicit-navigation coverage. These
checks establish browser behavior, not a person's visual approval or physical
phone acceptance.

At the person's request, 0.3.0 also adds one entrance per opening: a 900ms planet
glide, a 600ms window reveal staggered across 300ms, and the board's initial
half-flap fill. These are explicitly browser-opening adaptations. They do not
loop, block controls or assert missing work history; paused/reduced/hidden/stale
inputs do not trigger them, and ordinary updates do not replay them.
