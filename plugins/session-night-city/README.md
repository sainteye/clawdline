# Session Night City

A source-faithful browser port of [Night City](https://github.com/vib-z/herdr-layouts/tree/e4619fad4d5dea423fe1ebf2c296de7e6964f52d/night-city), `com.clawdline.session-night-city` version `0.3.0`. The five original SVG assets, glass palette, compact skyline geometry, ground alignment, moon and named attention rail replace the earlier independently drawn city. See [PROVENANCE.md](PROVENANCE.md) for the pinned source, unchanged files, MIT notice, formulas and every real divergence.

Projects stand for source spaces; Sessions are windows in one explicitly unpartitioned bank per Project. The v1 projection has no tabs, so the port does not invent floors. Native multi-tab tower heights, child windows, dead history, machine bridges and terminals cannot be reproduced. A readable Session detail/list replaces the original terminal area. This is a documented data-boundary adaptation, not a pixel-identical reproduction or an accepted visual design.

English and Taiwan Traditional Chinese follow the host language. All eight states have readable labels and symbols even when they share source glass geometry. Both completion states explicitly mean reported completion, not acceptance. Every `waiting_you` row stays in **Needs you / 需要你**, including stale readings. Opaque references determine stable ordering within an opening; labels are DOM text and never HTML. List selection and skyline selection agree; only the separate **Open Session / 開啟 Session** requests navigation. Unknown state, stale/unknown freshness and disconnected data cannot navigate. Reconnection requires a new snapshot.

Still mode, reduced motion, hidden context and disconnection stop animation; stale windows never animate. No live snapshots, history or preferences are persisted. Original dark palette remains under either host theme. The stage precedes the rail on mobile and only the skyline region scrolls horizontally. The original small glass dimensions are preserved; named attention/list buttons and the Open control provide 44-pixel touch targets. All rows remain available in the readable list.

## Rebuild

From the repository root, with an existing local Playwright installation:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-night-city
python3 tools/pack-view-plugin.py plugins/session-night-city web/console/public/view-plugins/session-night-city.zip
```

The cover reuses the actual source SVG artwork. The public renderer embeds a dedicated 128 × 128 RGBA8 PNG in the manifest. The public packer injects the unchanged v1 SDK and makes a deterministic, two-entry STORE ZIP (`manifest.json`, `index.html`). The HTML embeds original assets and the full MIT notice. Retained source/reference files are repository evidence, not additional ZIP entries. There are no external assets, network calls, paid APIs or new dependencies.

## Acceptance

```sh
node tools/ensure-web-deps.mjs
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
NIGHT_CITY_ORIGINAL_PREVIEW=/path/to/pinned/reference-preview.png \
PLUGIN_ARTIFACTS=/path/to/task/artifacts/browser \
node plugins/session-night-city/acceptance.mjs
tools/check.sh
```

The test imports the actual ZIP through the production manager, registry, store/parser, frame, bridge and SDK against a loopback-only synthetic source. Chromium and WebKit run at 1440 px in English and 390 px in Traditional Chinese. It compares actual rendered skyline dimensions against the inspected pinned pure source model, checks ground alignment and the early mobile stage, and saves original-versus-port, desktop/mobile, detail and list screenshots plus geometry JSON and results. Native upstream code is never run.

Checks also cover eight states, null Project, all named fresh/stale blockers, hostile Unicode/title/Project text, stable selected identity and ordering, same-data list, touch fallback, unknown/stale navigation refusal, still/reduced/hidden/disconnected motion, 80 Sessions with every blocker retained and contained overflow, empty snapshots, and explicit navigation. Visibility is a synthetic host event, not an actual OS background transition. Browsers and the local server are closed. This does not verify production installation, Cloud, actual mobile hardware or human visual acceptance. Full language/width cross products are not claimed.

## Motion in 0.3.0

The original cursor glyphs alternate at 600ms, attention lamps pulse at 1800ms,
and beams breathe at 3200ms. The attention sign opens and closes with the original
300ms height transition. Selection and unchanged snapshots preserve existing
animation phase; still/reduced/hidden/disconnected context cancels transitions.

Opening the view plays a one-time entrance when fresh data is visible and motion
is allowed. This is an explicit Clawdline presentation adaptation, not a claim
that upstream has this entrance: planets glide into place, city windows light
in sequence, and the board fills through its normal half-flap engine. Controls
remain available during the entrance; later snapshots do not replay it.
