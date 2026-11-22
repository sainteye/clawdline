# Sharing images

The repository provides English and Taiwan Traditional Chinese images for its public surfaces.

- `docs/assets/project-social-{en,zh-Hant}.png` is the 1280 × 640 GitHub repository Social preview artwork. GitHub stores one image per repository, so the English image follows the project default; the Chinese file is available separately. This setting is independent of README content.
- `docs/assets/readme-overview-{en,zh-Hant}.png` is the 1200 × 760 illustrated workflow in each README. It replaces the opening recording; the source GIFs and the two detailed collaboration diagrams remain available.
- `web/console/public/community-share-v2-{en,zh-Hant}.png` is the 1200 × 630 public community preview. List images follow the requested supported locale; topic images follow the original topic language. Other languages use English. Initial edge HTML and SPA navigation share the same selection.

Each PNG has an editable SVG source beside it. The image language applies to titles and explanatory labels, while product and project names retain their proper names. The marketing site owns its Security, AI-review, Clawdfather and documentation artwork in its separate repository.

PNG assets are rasterized locally with `@resvg/resvg-js` 2.6.2 and system fonts (Arial and PingFang TC). No image-generation API is needed. Public previews use versioned static image URLs so crawlers can retrieve the bytes without signing in. Existing v1 community assets remain available for previously cached links.

The README overview and cross-machine diagrams embed the canonical rounded orange
mark from `internal/domain/icon/brand.svg`, including its detached hands and solid
green underline. Regenerate the cross-machine diagrams with
`python3 tools/render-cross-machine-diagram.py`; render each editable overview SVG
with the pinned Resvg version above after changing its artwork. The diagrams retain
their existing text, dimensions and workflow layout.
On 2026-11-22 the overview's first column and corner label changed to name
Sessions, Bots and the voice copilot; that revision was rasterized from the SVG
with headless Chromium because Resvg was not installed locally.

The README highlight GIFs are recorded from the built console with synthetic data only, through
the Chrome DevTools protocol at 10 frames a second and encoded with an ffmpeg palette.
`bots-tower-{en,zh-Hant}.gif` is the Bots fixture (`web/console/tools/fixtures/project-assistants.html`)
at 1440 × 820: the Bot list beside the whole building, the elevator to 4F, and back.
`lantern-deep-{en,zh-Hant}.gif` is the console served by the voice demo's stand-in daemon
(`tools/copilot-demo/story.mjs`, six fictional Sessions plus example quota readings) with the
curated Lantern Deep package pinned beside the Session list, at 1440 × 900 scaled to 1080 wide.

The macOS notch draws the same rounded orange mascot natively through
`shell/darwin/RoundedBrandMascot.swift`. Its coordinates and warm-light gradient
follow the mascot group in `brand.svg`; the website's separate green line is not
part of the notch character. The existing routines still control its movement,
speed, blinking and sleeping expression. An unchanged copy of the bundled
`clawd.json` follows this brand update, including copies installed by earlier
versions. An edited pack or a different selected character keeps its own artwork.
Run `sh tools/test-rounded-brand-mascot.sh` for the selection and native rendering
checks; an optional directory argument also exports the expression and notch-scale
PNG samples.
