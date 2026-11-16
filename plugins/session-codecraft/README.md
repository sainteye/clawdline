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

In 0.2.0, the left command panel and minimap match the original composition.
The selected full title is a readable foreground control inside the
village viewport, outside the building cut-out layers. Original attention and
completion markers use separate non-overlapping touch targets; displaced markers
have leaders and crowded readings share an explicit count and selection list.
The complete title remains in the register and detail sheet. Phone selections
show the full title and explicit Open Session action in a modal sheet. The
landscape and original hero depth occlusion remain unchanged.
