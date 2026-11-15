# Session Territory 0.1.0

An independently importable View Plugin v1 candidate, ID
`com.clawdline.session-territory`. Original cartographic SVG/CSS artwork and binary
space partitioning show Project groups and labeled Session cells. Equal Session
cells carry **no effort, elapsed time, file touch, dependency or progress meaning**.
All eight host work states are readable text. Completion is a report, not acceptance.
The dark map palette is intentional in either host theme.

English and Taiwan Traditional Chinese follow host language. Selection survives
map/list switching and fresh snapshots while its opaque ref remains present.
Selection alone does not navigate. The explicit open button requires a fresh row;
failures are visible without automatic retry. Escape clears selection first, then
requests host close. The host's trusted return control stays outside the package.

Motion is limited to fresh working-state markers. Still mode, reduced motion,
hidden context, disconnect and stale/unknown rows stop those markers. Reconnection
waits for a new snapshot. Nothing is persisted and there are no external requests.

**File activity is unavailable in v1.** The visible boundary panel says this in
both languages. [data-contract.md](data-contract.md) records the bounded source
investigation, existing output-index seam and the review needed before a full file
layer. `activity.js` is a separate local synthetic renderer, not part of the ZIP
and not a live backend. The full file-activity feature remains blocked on that contract.

## Rebuild

Use repository-prepared dependencies; do not install dependencies into a shared
tree. With the existing local Playwright module path in `PLAYWRIGHT_MODULE`:

```sh
node tools/ensure-web-deps.mjs
node tools/render-view-plugin-covers.mjs plugins/session-territory
python3 tools/pack-view-plugin.py plugins/session-territory web/console/public/view-plugins/session-territory.zip
```

The public packer injects the unmodified v1 SDK at `CLAWDLINE_SDK` and writes exactly
two deterministic ZIP STORE entries: manifest and self-contained HTML. The cover
is a validated square RGBA8 PNG rendered from the authored `cover.svg`. No new
host catalog/card, SDK method, native command or capacity is introduced.

## Local acceptance

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node plugins/session-territory/acceptance.mjs /absolute/task/artifacts
tools/check.sh
```

The harness bundles the production manager, IndexedDB store, frame and bridge with
esbuild; imports the real ZIP using the manager file input; and exercises Chromium
and WebKit at widths 390/1440. All records are synthetic. It covers eight text
states, null Project, hostile labels, snapshot replacement, selection/list parity,
explicit navigation, stale/unknown freshness, empty results, reduced/hidden/still
motion, disconnect and layout bounds. A separate page renders synthetic file
activity with its synthetic heading, successful versus failed events and stale mode.
It starts an ephemeral loopback server, closes test pages/browsers and stops its
server. No real daemon, personal browser or transcript is used. Browser launch may
need the normal scoped sandbox permission on macOS.

Screenshots and `acceptance.json` are evidence for this local candidate, not
production Cloud, physical phone, native install or aesthetic approval. Root owns
review, commit/landing, host integration and any later publication. This child does
not install or enable the candidate in a person's production interface.
