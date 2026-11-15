# Session Orrery

Version **0.3.0**, package ID `com.clawdline.session-orrery`, is a source-derived
browser port of the Herdr Orrery at commit
`73bf616382d6b5c0ae5f73edf3cd122c4f51cda9`. This replaces the earlier independent
interpretation. See [PROVENANCE.md](PROVENANCE.md) for licenses, exact source
mapping, unchanged assets and every data/platform adaptation.

The engraved dial, sun, planet artwork, blocked/focus halos, walnut/brass mount,
IM Fell English typography and left-dome/right-eyepiece composition come from the
actual implementation. Projects evenly divide the **whole circle**. Within each
sector, the original FNV-derived hash places a Session at
`-π/2 + projectIndex × sector + (0.14 + 0.72 × hash) × sector`.
Radius shares remain `0.30` (waiting for you), `0.45` (reported milestone/work
completion), `0.60` or `0.71` (working, selected by the same hash), and `0.84`
(ready, holding, waiting for another Session, unknown). The complete register and
almanac retain all eight distinct readable states; shared artwork does not erase
their meanings. Reported completion is explicitly not acceptance. Almanac counts include stale
readings; the central awaiting-you count includes fresh readings only.

Project order is sorted opaque Project ref, including an unassigned Project;
Session longitude hashes the opaque Session ref. References are opening-scoped,
so no persistent identity or cross-opening position is promised. Adding/removing
a Project changes sector width, as it does upstream. Dense labels/points may
overlap: the complete, full-title register is the selectable fallback, with no
truncation of Session rows. Diagram names may ellipsize; list names wrap.

The eyepiece honestly shows Session selection/detail, **not a terminal**. Selection
alone never navigates. Only **Open Session / 開啟 Session** navigates, after checking
freshness, connection and known state. Unknown work state, stale/unknown freshness,
disconnection and reconnect without a new snapshot cannot navigate. The host
revalidates the target. The trusted host return control stays outside this package.

Only actual work-state changes have a 900 ms position transition. There is no
permanent spin, timer, inferred duration or elapsed-time growth. Hidden context,
reduced motion, still mode, stale data and disconnection stop motion. Escape first
clears selection, then requests close. Readable-list mode preserves selection and
all Session data. Mobile stacks the dial, eyepiece/register and almanac, uses a
smaller sun, and leaves diagram labels to the full readable register. Controls and
Session hit targets remain at least 44 pixels. English and Taiwan Traditional
Chinese follow the host language; both host themes retain the source dark palette.
All supplied titles/labels are safe text. No snapshots/history are persisted.

## Rebuild and acceptance

With repository-prepared web dependencies and an already installed Playwright:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-orrery
python3 tools/pack-view-plugin.py plugins/session-orrery web/console/public/view-plugins/session-orrery.zip
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs PLUGIN_ARTIFACTS=/task/evidence node plugins/session-orrery/acceptance.mjs
tools/check.sh
```

The archive is deterministic, exactly two STORE entries (`manifest.json` and
`index.html`), and self-contained. The SDK is inserted by the public packer. Fonts
and SVGs are embedded data URLs; no remote dependency or asset is fetched. Full
unchanged upstream font/source files remain beside the HTML; Basic Latin WOFF
subsets embedded in HTML preserve original outlines/metrics/kerning, without GSUB
ligature substitution. Other Unicode scripts use system serif fallback. The
subset procedure is preserved in PROVENANCE.md. The dedicated cover uses source
sun/planet assets with a simplified brass rim to meet the manifest bound.

Acceptance imports the actual ZIP through the production manager, store, frame,
bridge and SDK. Chromium/WebKit run at 390 px (Traditional Chinese) and 1440 px
(English), plus comparable 1600×900 captures. Geometry is compared with the
inspected pure `hash()`/`chart()` functions from retained upstream source, then
checked against actual DOM coordinates. All eight states, hostile Unicode/text,
null Project, 40-row density, empty snapshots, selection/list parity, explicit
navigation, stale/unknown/disconnected refusal, reconnect, transitions and motion
stops are covered. Host visibility is simulated through the production context
path, not claimed as an operating-system background-tab test. An optional
`ORRERY_ARCHIVE` path lets the same fidelity check reject the earlier archive.

Screenshots use visibly labeled synthetic data and establish fixture behavior.
Passing checks is not visual approval, a physical-phone test, a live Session test,
Cloud deployment or public Release acceptance.

Version 0.3.0 fits the desktop instrument to the visible iframe height and keeps
the eyepiece scroll inside its frame. The original 900ms state-change glide
remains; the dial never spins continuously. Work-age growth still requires a
trusted work-start input that v1 does not supply.

The instrument almanac uses the original four-row composition: awaiting you,
reported completion, working, and other states. The full eight-state counts
remain available in readable-list mode, and each Session keeps its exact state.

Opening the view plays a one-time entrance when fresh data is visible and motion
is allowed. This is an explicit Clawdline presentation adaptation, not a claim
that upstream has this entrance: planets glide into place, city windows light
in sequence, and the board fills through its normal half-flap engine. Controls
remain available during the entrance; later snapshots do not replay it.
