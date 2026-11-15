# Session Deadpan (`com.clawdline.session-absurd`)

Version 0.2.0 replaces the initial SVG character scenes with an original generated narrative
painting. The view is still an independent View Plugin v1 package, with the unchanged SDK and
only `sessions.read` and `sessions.open` capabilities. Galaxy, List and Pop are unchanged.

## Painting and live work

The telephone garden shows a person watering a telephone, a snail carrying a briefcase and a
worker sheltering a coffee cup with an umbrella. The artwork is intentionally absurd; it does
not encode progress or invent a Session state. Desktop Painting mode puts the complete picture
beside grouped live Sessions. On a phone they stack vertically. List mode hides the picture
and shows the same rows across the available width. Each row keeps a readable state, glyph,
Project and freshness; colour and artwork are never the only state signal.

Selecting a Session opens a compact detail panel. Only its explicit **Open Session** button
calls the host. Stale, unknown-freshness and disconnected readings cannot open and explain why.
Titles and Project labels are text only. No message, dispatch, classification or host scope
logic changed. The view still reads every authorized machine and Project. The trusted parent
return control remains outside the package.

The picture frame moves very slightly when motion is enabled. Manual stillness, reduced motion
and host invisibility stop every animation. The painting itself stays intact; no artificial
work progress or character motion is inferred from a Session.

## Authored assets and packaging

`painting.jpg` is a 900 × 1200 encoding of the original generated artwork. `cover.png` is its
40 × 40 square RGBA8 thumbnail, inside the existing manifest budget. Both source assets are
kept here for reproduction. No named artist's work is embedded or copied.

```sh
node plugins/session-absurd/embed-artwork.mjs
python3 tools/pack-view-plugin.py plugins/session-absurd web/console/public/view-plugins/session-absurd.zip
```

The embed command inserts the JPEG as a data URI and updates the dedicated cover. The public
packer still writes exactly `manifest.json` and `index.html`, with the SDK inlined. The archive
is 241755 bytes, below the existing 262144-byte bound. Neither the frame nor the cover makes an
external image request. Authoring files are not extra ZIP entries. Repacking is deterministic.

Sidebar → View plugins → Explore views → Install Deadpan uses the standard review/install flow;
ZIP import, replacement and rollback retain the shared registry behavior. Installation remains
browser-local. Host API, protocol, two-file archive and package ID are unchanged.

## Artwork direction

The original raster artwork was generated, then simplified into flat colours and clear outlines.
The scene brief was a sunny rooftop office garden with three calm workers, a telephone plant,
a briefcase-carrying snail and a coffee umbrella. Saturated cyan, green, ochre, coral and pale
pink areas, restrained expressions and impossible everyday objects carry the visual story.
The production asset is a compressed encoding of that generated image, not an SVG recreation.

## Verification

The production-host browser suite installs the ZIP, decodes the embedded 900 × 1200 JPEG in
Chromium and WebKit, checks mobile overflow, Painting/List switching, manual and host-requested
stillness, literal hostile labels, stale/unknown refusal and explicit navigation to the correct
machine. These are fixture checks; production installation and navigation require their own
Cloud browser acceptance. Compilation and CI are not visual approval.
