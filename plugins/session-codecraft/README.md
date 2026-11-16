# World of Codecraft for Clawdline

A browser port of Kevin Pardo's original fantasy village. Original day/night
landscape tiles, foreground masks, decorative heroes, frame sequences, anchors,
work/rest poses, smoke and warm light are retained. The scene can be dragged and
explored through its minimap. Select a hero or a full-name register row, then
explicitly open its Session. Mobile uses the same landscape with its own camera.

The initial port supplies six original heroes, assigned decoratively for this
opening. The v1 projection does not include agent programs, tool activity,
terminal contents, native recruiting or persistent character identity. It does
not claim a hero is testing, editing or fighting. Reported completion remains a
report, not acceptance. Every supplied Session remains in the searchable register.
Stale/unknown rows cannot navigate. Reduced motion, still/list mode, hidden host
and disconnection stop animation. All artwork is embedded; no remote resources,
upstream hooks or native commands execute.

`art/` contains original raster assets encoded as exact, lossless WebP. The
inventory records original PNG and encoded hashes. The generated `index.html`
and two-file STORE ZIP are built from `../herdr-scene.html`,
`../herdr-scene.js` and these vendored files:

```sh
python3 plugins/build-herdr-views.py
```

The original-art package exceeds the former 256 KiB host bound. A host with the
registered 32 MiB archive/expanded profile is required. The eight-package bound,
two-file STORE format, manifest/cover validation, CSP, scoped Session permissions
and all bridge bounds remain enforced. Installation is per browser and explicit.

See [PROVENANCE.md](PROVENANCE.md) and [the shared source-port record](../../docs/plugins/herdr-source-ports.md).

In 0.3.0, selection follows the original focused-agent model. A host-provided
focus flag selects and frames the matching hero when the view opens or that flag
changes. Selecting another hero glides the camera for 650 ms; a walking selected
hero remains horizontally followed until a drag, wheel, minimap, arrow or zoom
operation stops follow. Another selection resumes it. Reduced motion and still
mode frame immediately. Selected heroes retain the original ring and head bar.

The left command panel remains available. Hovering a hero reveals a nearby name,
Project and state; touch selection keeps that small foreground control available
with an explicit Open Session action, without opening a bottom sheet. Its position
follows the hero and camera, flips below when necessary, and stays within the
viewport. Status markers retain separate non-overlapping targets and crowd lists.
The persistent touch label and explicit navigation are browser adaptations: the
original uses a native tooltip and keeps a live terminal beside the village.
Opening a Session still closes the Clawdline view. Installation is explicit and
browser-local; an older installed package must be reviewed and updated.
