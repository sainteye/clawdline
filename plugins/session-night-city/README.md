# Session Night City

An independently installable View Plugin v1 candidate, `com.clawdline.session-night-city` version `0.1.0`. English and Taiwan Traditional Chinese follow the host language. The host offers this package in **View plugins → Explore views**; the normal package review is required and installation remains local to each browser or device.

Each Project is a building and each Session is a selectable window. Sessions without a Project share an explicitly named unassigned building. The permanent **Needs you / 需要你** rail names every `waiting_you` Session, including stale readings. A readable list shows exactly the same Sessions and preserves selection. On narrow screens selecting a Session brings its detail into view; only the separate **Open Session / 開啟 Session** button requests navigation.

All eight work states have distinct symbols and readable labels. `milestone_complete` and `work_complete` explicitly say **reported** completion; neither implies acceptance or an unseen completion. Striped windows with question marks represent stale or unknown freshness and retain their reported work-state symbol. Detail and list rows spell out freshness. Stale/unknown readings cannot navigate. Fresh working windows have a subtle typing animation; still mode, reduced motion, hidden context and disconnection stop it. Reconnection requires a new snapshot before retained rows can resume motion or navigation. Theme remains intentionally dark.

Ordering uses opaque Project and Session references within an opening, not titles or received array position. A selected reference survives complete snapshot replacements and title changes; removal clears selection. Reopening gets new host references and does not claim historical ordering. No rows are truncated. The skyline scrolls within its own region when needed, and the named rail scrolls independently. No Session snapshot or still-mode preference is persisted.

## Build

From the repository root, using an already installed local Playwright module:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-night-city
python3 tools/pack-view-plugin.py plugins/session-night-city web/console/public/view-plugins/session-night-city.zip
```

The SVG is original editable artwork. The renderer produces the dedicated embedded 128 × 128 RGBA8 PNG in the manifest; it is not a third archive entry. The public packer embeds the unmodified v1 SDK at the marker and emits deterministic STORE entries, `manifest.json` and `index.html`. The package contains no remote assets, fonts, dependencies or external API calls.

## Acceptance

Prepare existing repository dependencies with `node tools/ensure-web-deps.mjs`; do not manually install packages into shared dependencies. Then run:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
PLUGIN_ARTIFACTS=/path/to/task/artifacts/browser \
node plugins/session-night-city/acceptance.mjs
tools/check.sh
```

The acceptance script bundles the real production manager, registry, frame, bridge and SDK. It serves a loopback-only synthetic source, imports the actual candidate ZIP through file input and opens it without editing the host catalog. It closes its own browsers and server. It runs Chromium and WebKit at 1440 px in English and 390 px in Traditional Chinese, with screenshots and `results.json` in the chosen artifacts directory.

Each run checks all eight states, null Project, named fresh and stale blockers, initial and replacement snapshots, hostile title/Project text, stable selection and ordering, list equivalence, 44 px window targets, stale/unknown navigation refusal, still/reduced/hidden/disconnected animation, 80 retained Sessions with contained overflow, empty snapshots and deliberate navigation. Visibility is synthetically supplied through the production host's visibility event, not an actual operating-system background transition. The compact matrix does not claim all language/width cross products, native mobile hardware, production installation, Cloud acceptance or multi-machine authorization coverage.

The repository gate validates every authored package and its dedicated cover
independently from the curated catalog. Curated entries have a separate coverage
check; adding this candidate does not add it to the host's recommended list.
A browser pass alone does not replace the required repository commit gate.

## Data boundaries and references

Only the authorized v1 projection is used: opaque references, Project reference/label or null, Session title, work state and freshness. There are no machine identities, tabs, panes, terminal content, elapsed timestamps, progress percentages, dependency edges or accepted-completion records. Host return and authorization remain outside the package. Names use DOM text nodes and are never interpolated into HTML.

The upstream [Night City recipe](https://raw.githubusercontent.com/vib-z/herdr-layouts/main/night-city/RECIPE.md) informed the skyline and named-attention concept only. Its artwork, code, fonts, history and native APIs are not included. All SVG/CSS artwork here was authored locally; no upstream license is relied upon for copied assets.
