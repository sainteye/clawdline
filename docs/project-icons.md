# Project icons

A project icon has an RGB `accent` and either a colour grid (`cells`) or a
self-contained SVG (`svg`, with empty `cells`). The daemon resolves it for
the console's project, Session and Board views. Application and assistant marks
are separate assets.

The application mark uses D-01 + C-03 + H-04: equal rounded-square tiles,
soft original-orange lighting and detached hands above the continuous terminal-green
underline (`#4CCB7F`). `internal/domain/icon/brand.svg` is the canonical artwork.
It retains the selected geometry and shading, with a cropped viewBox for compact use.

`web/console/src/brand-mark.ts` exposes that artwork to navigation, sign-in,
support, administration, the working indicator and machine-workspace marks.
The console loads it as an isolated SVG image; no page CSS recolors the mascot.
The generated `brand-art.js` module also works in synthetic host builds that
have no SVG file loader. Compact marks use the static, solid green line.

`internal/domain/icon` reads the same rounded rectangles and lighting field to
render browser, home-screen, macOS application and launch-screen assets.
Regenerate the assets and console module with
`go run ./internal/domain/icon/cmd/splashgen -brand`; `-brand -check` verifies
that committed outputs still match the renderer.

## Rounded underline motion study

The [rounded-brand light-flow SVG](assets/brand-mark-rounded-flow.svg) keeps the
mascot still and moves a periodic light band left to right through the existing
green line in a 1.2-second loop. The base line remains solid; reduced-motion CSS
hides the light layer. This is a separate large-art study. The local application
uses the static artwork, and this change does not deploy the production console
or the marketing website.

## Existing large-format studies

Large-format flow concepts retain the fine body grid and explore the underline:
[one-way flow](assets/brand-line-concepts/f-01.svg),
[packet pulses](assets/brand-line-concepts/f-02.svg),
[two-way exchange](assets/brand-line-concepts/f-03.svg), and
[quiet shimmer](assets/brand-line-concepts/f-04.svg).
The [animated comparison](assets/brand-line-concepts/flow-comparison.gif) shows
the four transmission treatments together. These are design drafts, not selected
production assets. Small marks keep the solid line rather than the lighting effects.

F-01 and F-04 also have standalone, dark-background animation drafts for a
possible homepage illustration. Both are 832 by 608 pixels at 20 frames per
second, with the body held still and only the underline moving. The repeated
endpoint matches the opening frame before encoding; the endpoint is not included
twice in the loop.

| Concept | Loop | GIF preview | Animated WebP alternative |
|---|---|---|---|
| F-01, one-way flow | 2.4 seconds, 48 frames | [GIF](assets/brand-line-concepts/f-01-loop.gif), 837,714 bytes | [WebP](assets/brand-line-concepts/f-01-loop.webp), 107,244 bytes |
| F-04, quiet shimmer | 4 seconds, 80 frames | [GIF](assets/brand-line-concepts/f-04-loop.gif), 1,292,044 bytes | [WebP](assets/brand-line-concepts/f-04-loop.webp), 132,428 bytes |

These files are previews and have not been wired into the website. Keep a static
poster available if an animation is later adopted for a reduced-motion setting.

The revised motion study sends both highlights from left to right at constant
speed. F-04 no longer reverses direction. Each highlight continues from the left
after exiting the right edge; the periodic light copies preserve the loop seam.
The body and its fine grid remain unchanged. These revisions run at 25 frames per
second; the earlier drafts above are retained for comparison.

| Revised concept | Loop | GIF preview | Animated WebP alternative |
|---|---|---|---|
| F-01, fast left-to-right flow | 1 second, 25 frames | [GIF](assets/brand-line-concepts/f-01-fast-ltr.gif), 474,174 bytes | [WebP](assets/brand-line-concepts/f-01-fast-ltr.webp), 56,108 bytes |
| F-04, soft left-to-right flow | 1.2 seconds, 30 frames | [GIF](assets/brand-line-concepts/f-04-fast-ltr.gif), 858,630 bytes | [WebP](assets/brand-line-concepts/f-04-fast-ltr.webp), 69,304 bytes |

## Project icon sources

`internal/domain/icon` resolves a local mark in this order; `Registry.Resolve`
reports the source as `mirrored`, `override`, `registry` or `generated`:

1. A mark mirrored from the owning machine by
   [project settings sync](project-sync.md), attached with `SetMirror`.
2. A saved mark in `CLAWDLINE_NEXT_DIR/project-icon-overrides.json`, using the
   longest containing path. Existing overrides remain readable so an upgrade
   does not make a chosen mark disappear. The console no longer creates them.
3. The longest containing entry in `~/.claude/project-icons.json`. This
   compatibility input remains read-only. A home-directory entry names only
   the home directory, not every project below it.
4. A deterministic creature generated from the working directory's FNV-1a
   hash.

The Project health check measures whether a project-specific identity is set.
Built-in generated creatures remain incomplete even when a legacy registry
entry selects their hue, tone and shape, or their pixels have been copied or
mirrored from another path. The displayed mark and stored data stay intact.
`setup.icon` reports `generated` for this artwork; other values identify the
source of a project-specific mark. Resolve still returns the actual storage
source. Custom pixel art and SVG logos count as configured identity. The check
recognizes the built-in colour grids, not visually similar artwork encoded as
SVG, and does not judge the quality or recognisability of a custom logo.

The setup assistant should first reuse an existing Project logo. If none is
available, it asks the person to specify an icon rather than registering another
default creature to complete the checklist.

The daemon rereads the override file when resolving a Project or Session icon.
Creation, edits, atomic replacement and removal take effect on the next read;
the legacy registry is also reread and compared by content. A malformed or
unreadable setting keeps the last valid mark in memory, while Project and
Session list requests report `icon_store_unavailable` (HTTP 503) instead of
claiming a successful refresh. No daemon route writes the override file;
`Registry.Save` exists but has no production caller.

Run `clawdline project refresh [directory]` to ask the running daemon to read
one listed Project's current display state. The directory defaults to the
current directory. It reads the Project list, including its icon and source,
repository identity, deployment activity, server declarations, sync readiness
and unify status, then checks the Session list presentation. It only reads:
it does not start or restart a server, run a deployment, or change settings.

For example, after changing an icon file:

```sh
clawdline project refresh /path/to/project
```

Success prints the path, current accent and icon source, repository identity,
deployment and server status, sync readiness, and unify status, then a refreshed
status and exits 0. Human-facing labels use the CLI language. A directory outside the current Project list
reports that it is not listed and exits 3 (unknown); the list is limited to
the 40 most recent places. An unreadable or invalid file, daemon refusal,
network error or invalid answer reports an error and exits 1. The command
never reports success on those errors.

For projects on multiple machines, [project settings sync](project-sync.md)
matches checkouts by their portable git `origin` and carries the source's name
and icon to each mirror. A receiving machine follows later source changes when
the Projects page is opened. A local path, folder name or generated path hash
is not a cross-machine project identity.

## SVG and pixel input

The compatibility registry accepts SVG in an entry's `art` object:

```json
{
  "projects": {
    "/path/to/project": {
      "art": {
        "accent": "#D97757",
        "svg": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\" fill=\"#D97757\"/></svg>"
      }
    }
  }
}
```

SVG needs a finite, positive-size `viewBox`. Static shapes, groups, gradients,
clipping, masks, text and local `#fragment` references are supported. Scripts,
event handlers, external resources, embedded HTML, stylesheets and animation
are refused. Use presentation attributes or simple inline styles. An SVG is
limited by the existing 96 KiB icon request budget. The console decodes it as
an isolated image, never inserts its markup into the page, and discards a late
image decode if the canvas has since been assigned another icon.

Pixel grids accept at most 64 rows and 64 columns (4096 cells), with RGB colours
or transparent cells. Legacy palette art follows the same dimensions and colour
validation; shorter legacy rows retain transparent/background padding. Invalid
registry edits keep the last valid marks and make refresh report an error.

All views fit project marks proportionally into the existing eight-cell slot
at the caller's scale. Ordinary small pixel marks keep their original size;
larger marks shrink without cropping. SVG uses the same slot with smooth
rendering. The accent remains independent of the SVG's fills. Storage and
project-settings sync carry SVG alongside the accent; receiving machines need
a version that supports SVG to draw it.
