# Territory source study and isolated browser renderer

This study delivers a source-derived **synthetic reference prototype**, not a
production file map. The shipped Project/Session overview is not equivalent to
Territory. Its equal Session rectangles cannot produce file areas, file heat,
read trails or concurrent-touch observations from the current v1 projection.
Rendering fidelity and permission to observe real file activity remain separate
outcomes. Passing the checks below is not visual acceptance.

Task base and unchanged Git HEAD: `474e125c377d970b323018cda31417420c88ede4`.
Only the ten assigned study/prototype/vendor files are authored. No production
`index.html`, manifest, cover, ZIP, host, SDK, catalog, API or capacity changes are
included. The task prohibits child commits; the root must review and commit these
uncommitted bytes. No daemon restart, installation, deployment or paid API call
was performed. No personal transcript or workspace inventory was read.

## Pinned source and license

Upstream is [adamentwistle/herdr-territory at b53947d](https://github.com/adamentwistle/herdr-territory/tree/b53947d6b9ebe10208d1b7edf2948d3e68dd3086),
commit `b53947d6b9ebe10208d1b7edf2948d3e68dd3086`. The supplied extracted
reference includes `mods/territory/{treemap,activity,frame,view,crew,main,calls,poll,demo}.js`,
`territory.py`, `layouts/territory.json`, the treemap/activity tests,
`preview.svg`, `reference-preview.png`, `mod.json` and `LICENSE`. These were read
as source data. No upstream application, Python helper, installer, hc command,
exec handler or transcript scanner was executed.

The license is **MIT, Copyright (c) 2026 Adam Entwistle**, retained in full at
[upstream/LICENSE](upstream/LICENSE). The adapted renderer also carries the full
notice. Exact unchanged source copies and SHA-256 digests follow:

| Retained file | Original path | SHA-256 |
| --- | --- | --- |
| `upstream/LICENSE` | `LICENSE` | `242327db098c743029c47659bdad4a0bdfe9991734f72ad46931cb59491b2178` |
| `upstream/treemap.js` | `mods/territory/treemap.js` | `d5d3f55bc5ddfc1ddff4a538b285a1df56321e931b55cf4b43eba41effad816d` |
| `upstream/activity.js` | `mods/territory/activity.js` | `86e1fa9eb9aa4c01f5e49ebd065f52516da3ccbbe6417c83e4b6fa993502b6c3` |
| `upstream/frame.js` | `mods/territory/frame.js` | `5c904a502a65d18a1ada6b1921cf8e1c0f413259af813273c6767cade82347da` |
| `upstream/view.js` | `mods/territory/view.js` | `c46ce779839ebb76d821184681f8398de09538a88001e78f162158d7f5736982` |
| `upstream/crew.js` | `mods/territory/crew.js` | `ac19dc9036b9c184fb55d8fd4ba99d7b4808492436f0edb0e846ee3176ca1b07` |

The prototype imports unchanged `treemap.js` and `activity.js`. Its frame geometry
is copied from `frame.js`, with the activity import redirected and `MAX_DOTS = 12`
supplied directly so no native crew/transcript adapter is loaded. `view.js` is
translated into DOM/CSS/SVG. The upstream synthetic `demo.js` fixture is retained
inside the adapted renderer under the MIT notice; it supplies the same 60-file
harbour story at step 7. `syntheticFixture` converts only that fictional fixture's
four tool names and fictional root into activity touches. This conversion is not
a validator or adapter for real events.

The unchanged `view.js` and `crew.js` are audit references, not browser entry
modules: they retain upstream GPUI/Herdr and `calls.js` imports. They must not be
loaded as an installation or executed to obtain real data. The original preview
is preserved by the root's reference extraction and comparison artifacts; no
native terminal asset is drawn into the port. Territory has no bundled font or
bitmap artwork used by this map. `preview.svg` is a gallery thumbnail, not the
renderer. Native `theme()` tokens and configured font/zoom are external inputs;
their exact settings are not recorded in the supplied preview.

## Source-to-current mismatch

| Concern | Pinned upstream evidence | Current product at task base | Result |
| --- | --- | --- | --- |
| What a cell means | `treemap.js:47` `buildTree(files, rest)` takes relative file paths and bytes | `index.html:17` `split` recursively divides Session rows equally within Projects | Different domain and geometry; opaque Session/Project refs are not filenames |
| Area and hierarchy | `treemap.js:18` `weightOf = cbrt(max(bytes,256))`; `squarify` at line 76; folder squeeze, 16px headings, 2px gaps, small-file groups | Equal Session area, no byte sizes or directory tree | Current overview cannot stand in for a file map; the old equal-area proposal would also diverge |
| Heat | `activity.js:42` heat starts at 0.7, lifts 0.15 per fresh write, fades over 5 minutes; rounded to tenths on 15-second beats | No file writes or event times | Cannot derive from `working` or host observation freshness |
| Trails and positions | `trailOf` keeps six read/scan points for two minutes; `lastTouch` docks after ten minutes | No last file, read events, scan events or timestamps | Cannot place a Session on a file or infer a trail |
| Concurrent touches | `collisions` at line 83 uses a two-minute window, at least one writer and distinct agents; parent-child excluded | No file identity, timestamped touches or authorized parent relation | Not a lock, proven edit conflict or completion signal; no such claim is available |
| Visual hierarchy | `frame.js` has 44px header, 28px dock, 32px dot hit box, 280px card; `view.js` layers folders, tiles, heat, trails, dots, detail | Current overview has independent panels, large Session text and status legend | The prototype preserves map geometry and layering; production remains unchanged |
| Data acquisition | `main.js` invokes hc/Python, reads calls every third second and inventory every 30 ticks; `territory.py` reads git inventory and provider logs | Opaque browser frame with only subscribe/open/close | Native acquisition cannot be copied across the v1 sandbox |
| Terminal and agent focus | Layout places a native terminal workspace below the map; `pressDot` runs native pane focus/terminal peek | Only explicit validated `sessions.open` navigation | No terminal is simulated. Synthetic agent selection is followed by an explicitly disabled Open Session control |

`data-contract.md` remains the prior investigation, not a new implementation
contract. Its statement that the prior candidate used only the concept describes
that older renderer; this study now copies and retains upstream code. Its
equal-area file proposal cannot satisfy the source's weighted-file layout.

## What the authorized projection actually supplies

[bridge.ts](../../web/console/src/view-plugins/bridge.ts) lines 4–18 define the
eight work states, freshness, `SourceSession`, `ViewReading` and context.
`snapshot()` at lines 41–65 mints per-opening opaque refs and projects only
`ref`, title, Project ref/label, workState and freshness. Its method allowlist
accepts subscribe, explicit open and close; no file API crosses it. Unknown,
stale and disconnected rows cannot establish a safe navigation target. The
prototype does not use this bridge and cannot open any Session.

[sources.ts](../../web/console/src/view-plugins/sources.ts) lines 10–21 map local
and Cloud Session summaries. `fleetPluginSource` at line 66 namespaces source
identity and retains source connectivity/partial readings. Cloud observation time
is used to assess summary freshness, not emitted as file activity. Neither a
Project basename nor an opaque UUID supplies a directory tree, byte size or event.

The existing output index is a useful, narrower **host-side** seam, not an already
authorized plugin capability:

* [sessionfiles/store.go](../../internal/adapters/sessionfiles/store.go) lines
  38–62 define `File`, including paths, size, times, source, transcript anchor and
  content revision. Lines 78–83 define `Listing` with `More`, `Partial`, `Coverage`.
  `merge` at line 244 aggregates observations per output and deduplicates native
  versus explicit publication. `List` at line 351 scans, inspects and saves an
  index; a read request is not proof of a mutation-free backend operation.
* [sessionfiles/scan.go](../../internal/adapters/sessionfiles/scan.go) lines 20–110
  restrict receipt scanning to Claude/Codex, distinguish published-only,
  unavailable/oversized records and incomplete scans, and enforce workspace and
  symlink boundaries. `nativeReceipts` at line 231 matches direct Codex patch calls
  to successful output. Shell stdout and intended calls do not count as writes.
  The index does not capture general reads, arbitrary shell/editor writes or all
  repository files; it also cannot reconstruct every historical event from its
  aggregated rows. `UpdatedAt == 0` cannot create recent heat.
* [session_outputs.go](../../internal/transport/http/session_outputs.go) lines
  38–118 admit a pinned execution before and after listing, use conversation
  identity and machine-only POST publication. Those existing checks remain
  necessary; raw `File` must not be passed into a plugin, and this task called
  none of these endpoints.

These are five distinct things: git inventory describes files that may be
untouched; an output index lists observed/published products; successful tool
events describe particular operations; raw transcript text contains much broader
private content; times and coverage determine what an observation can support.
Publication is not automatically a write event, mtime is not tool execution, and
no observation is not evidence of inactivity.

Upstream itself has observation limits: `territory.py:201` `file_calls` records
tool-use timestamps and removes calls only when an error result is found within
the bounded log tail. It does **not** require a matched successful completion for
every retained call. A pending/missing result can survive. Its git-index stamp may
miss a pure content-size change; sizes beyond its scan budget are guessed, and
untracked discovery can time out. Tail/event caps limit coverage. These limitations
must not be promoted into trustworthy host provenance. Likewise, `crew.js`'s
90-second subagent silence heuristic is not authority to infer death/completion.

Live-data investigation stopped at these definitions. No personal record was
opened, no filesystem inventory operation ran and no backend extension was made.

## Narrow extension the root would need to own

This is a proposal requiring a separately reviewed opt-in, not new permissions
granted by `sessions.read` and not an instruction to implement them now.

1. Review two narrow metadata capabilities, provisionally
   `files.inventory.metadata` and `files.activity.metadata`. Grant them for an
   explicit Project on an authorized source and pinned execution, separately from
   the current all-Projects Session summary grant. Names themselves disclose
   information; show which relative paths and byte metadata will be shared.
   Define local and Cloud authority, expiry and immediate revocation together.
2. Inventory metadata needs per-opening opaque file/Project refs, authorized
   relative display paths, bytes with `known|estimated|unknown` quality, inventory
   revision, excluded/omitted totals and complete/partial/unavailable status.
   Real byte weights are required for the upstream area formula. Unknown sizes
   need an explicitly identified group or unavailable layer, not silently equal
   file areas advertised as source fidelity. Keep absolute paths, filesystem
   project keys, symlink destinations and content private to the host.
3. Activity metadata needs deduplicated event ref, file ref, Session ref, operation
   (`read|write|scan`), outcome (`succeeded|failed|pending|unknown`), nullable trusted
   occurrence time, observation time and provenance. Only matched successful
   observations may enter heat/last-touch/trails; failed/pending/unmatched events
   retain explicit coverage semantics, never fake heat. Do not convert explicit
   publication into tool success. Define rename/delete and execution replacement.
4. Prefer forward-only host receipt extraction after opt-in, emitting metadata
   only. If receipt extraction internally reads provider logs, that read authority
   must be explicit too: emitting no transcript text does not mean the host read
   none. Historical backfill needs its own reviewed authority/window. Do not ship
   a generic transcript reader, REST client, exec channel or raw tool input/output.
5. Define source clock/skew and trusted occurrence-time policy before applying
   2/5/10/15-minute formulas. Observation time cannot substitute for missing event
   time. Parent/owner relations need their own authorized provenance if upstream
   parent-child collision exclusions are retained. Without those, concurrent
   touch eligibility is unknown, not a proven conflict.
6. Root-owned scopes include sessionfiles/new metadata event adapters,
   authenticated HTTP and Cloud transport, schema/SDK/bridge/package capability
   allowlists, grant management and registered retention/payload capacities.
   Preserve namespaces across equal paths/sources and recheck authorization after
   asynchronous work. Revoke active frames and clear file mappings on changes.
   Disconnect is immediately stale; reconnect requires a new authorized snapshot.

Acceptance must distinguish full inventory from observed outputs and unsupported
providers; prove read/write success versus failures, pending/missing results,
shell/editor writes, missing/invalid/skewed time, partial/truncated records, equal
paths on different sources, replaced executions, symlink escape, rename/delete,
revocation during delayed reads, disconnect/reconnect and authorized empty data.
Verify real opt-in local and Cloud readings separately from synthetic fixtures.
If the root chooses only an output view, name it **observed outputs** and omit
claims of full Territory heat/trails/collisions.

## Prototype fidelity and deliberate differences

`upstream-renderer.js` reproduces the original squarify/cube-root layout, grouped
small files, header/dock/card dimensions, dot fan/clamping/labels, 1.5px dashed
trails, heat opacity `0.14 + 0.76 * level`, and the story's parent-aware concurrent
touches. At comparable desktop scale, the screenshot uses the original 1600px
preview's apparent 1.2 zoom, scaled to 1440px. At 390px, native logical pixels are
used and the treemap reflows using the same upstream algorithm. It is not a
scaled-down desktop screenshot.

Remaining divergences are explicit:

* The map has synthetic labels above it, a readable file list, eight-state
  vocabulary and disabled Session navigation below it. These are reference/safety
  scaffolding, not a new production layout or extra live observations.
* Colors approximate the supplied preview's dark theme because upstream calls
  dynamic Herdr `theme()` and no exact active theme was supplied. System sans-serif
  and system monospace replace platform/configured font metrics. This is not a
  pixel-perfect theme claim. Light-theme parity remains unverified.
* DOM/SVG/CSS replace GPUI nodes. Working halos and collision opacity preserve
  their intent and periods; there is no GPUI spring solver, no continuously
  advancing synthetic clock, and no automatic replay. Still mode starts on.
  Data changes are driven explicitly by fixtures; fixed screenshots use one step.
* Native layout fold becomes local clipping. It does not resize a terminal
  workspace or persist height. The absent native terminal is stated plainly.
* Synthetic dots select a reference agent; Open Session stays disabled. The
  production explicit-open behavior is untouched and must be retained by any
  later real adapter. Unknown/stale/disconnected reference data never navigates.
* Full Unicode is retained as safe text; small tiles truncate visually but expose
  full labels and a readable list. This intentionally avoids upstream's name
  slicing and terminal-address fallback, which are not v1 data.
* Stale/unknown/disconnected contexts withhold heat/trails/collisions immediately;
  hidden/reduced/still stop CSS motion. Reconnect alone cannot revive activity:
  a fresh synthetic fixture is required. Nothing is persisted.

## Reproducible evidence

Run with an existing Playwright module and authorized output/reference locations:

```sh
ARTIFACT_DIR=/absolute/task/artifacts \
PLAYWRIGHT_MODULE=/absolute/existing/playwright/index.mjs \
UPSTREAM_REFERENCE_DIR=/absolute/pinned/territory-files \
node plugins/session-territory/upstream-preview.mjs
```

The runner uses a temporary HTTP server bound only to `127.0.0.1`, serving this
plugin directory. It stops the server and closes every task-owned page/browser.
No new dependency is required by this prototype. Its tests verify byte formula,
explicit squarify coordinates with floating-point tolerance, pixel bounds,
grouping, heat/trail/collision windows, unchanged-source hashes and exact
representative frame parity. Browser checks inspect actual tile geometry in both
engines at 390/1440, en/zh-Hant-TW, card/Escape, selection and disabled navigation,
eight readable states, touch/list fallback, hidden/reduced/still, unknown/stale/
disconnected, reconnect snapshot fence, hostile Unicode/markup text, empty data
and absence of external requests.

Task artifacts include `original-versus-port-1440.png` (whole reference and port),
`original-versus-port-map-1440.png` (aligned map crops), twelve engine/width/language
or disconnected screenshots, `prototype-results.json`, exact prototype logs and
`local-gate-final.log`. These artifact names are relative to the task's artifacts
directory; the root owns Cloud publication and visual review. The original is a
supplied screenshot, not a newly launched Herdr instance.

Initial sandbox browser launches failed (Chromium Mach port permission, WebKit
abort). Both engines passed a DOM probe after scoped escalation. The first
prototype run exposed an overly exact floating-point assertion; the second
exposed Escape losing its container event target after rerender. Both were fixed,
and the earlier failed logs are retained. A complete final matrix is recorded in
`prototype-check-final.log`. `tools/check.sh` is the required local gate; its exact
outcomes are retained independently of browser evidence. The gate may prepare the
repository's already pinned dependencies in the isolated worktree; no dependency
manifest or lockfile is changed. No package hash changed because production
packages were deliberately untouched.
