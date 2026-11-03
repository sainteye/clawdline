# Sharing images

The repository provides English and Taiwan Traditional Chinese images for its public surfaces.

- `docs/assets/project-social-{en,zh-Hant}.png` is the 1280 × 640 GitHub repository Social preview artwork. GitHub stores one image per repository, so the English image follows the project default; the Chinese file is available separately. This setting is independent of README content.
- `docs/assets/readme-overview-{en,zh-Hant}.png` is the 1200 × 760 illustrated workflow in each README. It replaces the opening recording; the source GIFs and the two detailed collaboration diagrams remain available.
- `web/console/public/community-share-v2-{en,zh-Hant}.png` is the 1200 × 630 public community preview. List images follow the requested supported locale; topic images follow the original topic language. Other languages use English. Initial edge HTML and SPA navigation share the same selection.

Each PNG has an editable SVG source beside it. The image language applies to titles and explanatory labels, while product and project names retain their proper names. The marketing site owns its Security, AI-review, Clawdfather and documentation artwork in its separate repository.

PNG assets are rasterized locally with `@resvg/resvg-js` 2.6.2 and system fonts (Arial and PingFang TC). No image-generation API is needed. Public previews use versioned static image URLs so crawlers can retrieve the bytes without signing in. Existing v1 community assets remain available for previously cached links.
