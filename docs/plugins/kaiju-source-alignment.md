# Kaiju City source alignment

The reference is [Jiqize/herdr-kaiju at ba44d36](https://github.com/Jiqize/herdr-kaiju/tree/ba44d36dafd8a5ba757513253ea1eac2934a67b6), also its HEAD when checked. The 0.2.1 browser package retained the city artwork and damage metaphor but used a generic register and omitted several original interactions. Version 0.3.0 restores these details through the existing read/open/close SDK.

| Original detail | Browser behavior |
| --- | --- |
| First-appearance monster assignment (`state.js`) | Cycle through all eight original monsters, including idle roster members. Reordering rows preserves monster identity and Project slot order for this opening. |
| Entry and retreat (`city.js`) | Newly active monsters travel from the original sea entry over 900 ms; idle/holding monsters return there. Initial actors start at their standing points. Removed refs disappear immediately rather than retaining an actionable ghost. |
| Moving bodies and hit regions | Walking monsters render above buildings, have no hit target and suppress bubbles. Settled hits use the original 36-pixel width and visible sprite height, in depth order. |
| New buildings rise | New Projects rise from 40 pixels below the final position after the original 40 ms delay, with a 520 ms response and 0.62 damping. Clip at the building base; do not replay construction on first observation. |
| Impact and attention effects | Retain the 180 ms monster shake, 240 ms building shake, 700 ms dust and 500 ms siren periods. Building effects follow settled on-stage monsters. |
| Active space arrow | Local Project selection or host Session focus selects its building and original roof arrow. Follow the current damage-layer roof, transition vertically over 400 ms and float three pixels over 900 ms. Select another district/cast when needed. |
| Dossier cards and avatar tiles (`roster.js`) | Restore numbered folder tabs, original clipped portraits, state stamps, Project grouping, paper/night palettes, hard shadows and the mode switch. Embed the unmodified Fusion Pixel font with its OFL notice. |
| Dossier fields | Show actual title, Project, state, freshness, visual wear and supplied elapsed state time. Working duration requires a trusted turn timestamp. Confirmed callback waits have a separate caption. |
| Background news (`news.js`) | First observations create no news. Fresh background transitions into attention or reported completion produce three cards and an overflow count. Selection, host focus, state exit, disappearance and Clear remove entries. Reconnection does not invent unseen transitions. |
| Repair and new turns | Retain 650 ms idle repair steps. New working turns take their supplied duration immediately. Focus never accepts completion or changes work state. |

## Intentional host differences

The browser retains a responsive full-screen composition rather than the native 400/76-pixel sidebar beside terminals. Herdr places the city beside native terminals; clicking a monster focuses its pane. The full-screen browser view keeps selection separate from **Open Session**. A nearby touch control and an optional details section provide that action without automatically opening a phone sheet. Idle Sessions stay in the roster; selecting one points to its Project and keeps an inline action.

Native program names, tab capsules, pane headers, space creation, window shortcuts and persistent daily work/destruction statistics are outside this SDK. The port does not guess provider identities or fabricate history. Supplied timestamps support elapsed-time text, not a reconstructed work record. Roster mode and decorative assignments last for this opening.

The original stops placing spaces after eight slots and stacks overflow monsters. The port retains districts and explicit casts so every authorized Project and Session remains reachable at the existing 256-Session bound. Still/reduced-motion, hidden and disconnected states stop motion. Stale or unclassified readings cannot navigate; a fresh projected callback wait remains distinct from an unknown work state. Permissions and automatic navigation remain unchanged.

## Reproduce

The original source and geometry remain in `plugins/session-kaiju/upstream/`. `art-inventory.json` binds original PNGs to lossless WebPs; `font-inventory.json` identifies the unmodified source font.

```sh
python3 plugins/build-herdr-views.py
python3 tools/build-view-plugin-catalog.py
SCENE_KAIJU_PARITY_ONLY=1 SCENE_VIEWS=kaiju \
  PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
  PLUGIN_ARTIFACTS=/absolute/task/output \
  node plugins/herdr-scenes.acceptance.mjs
```

The matrix uses the production registry, frame and SDK in Chromium/WebKit at 390 and 1440 pixels. It tests supplied times, state transitions, pixels, roster modes and source motion. Ordinary scene and density suites remain separate regressions. Synthetic fixtures do not establish physical-phone acceptance or native Herdr capabilities.
