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
