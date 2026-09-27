Made the page consistent with the rest of the site and fixed what was actively hurting it in search:

- **`<html lang="en">`** was missing — added, matching your other pages.
- **`<title>`** was just the site name ("Iron & Ember") — now it's keyword-descriptive: "How to Season a Cast Iron Skillet in the Oven | Iron & Ember", which is what shows as the clickable headline in Google results.
- **Meta description** was absent, so Google was writing its own snippet — added one describing the method (helps click-through rate).
- **Canonical link** was missing — added, pointing to the post's own URL.
- **The post title was a `<div>`, not an `<h1>`** — the page had no top-level heading at all, which weakens Google's read of the page's topic. Changed to `<h1 class="post-title">` (same CSS class, so no visual change) and wrapped the date in `<time datetime="2026-03-14">` for cleaner semantic markup.
- **Both step images had no `alt` text** — added descriptive alt text (image search and accessibility both benefit).
- **"click here" link** to the cornbread post — replaced with descriptive anchor text ("skillet cornbread with brown butter"), since link text is a ranking signal for the linked page.

One thing I didn't touch: `ironandember.example` is a placeholder domain used across the whole site (sitemap, robots.txt, canonical tags) — you'll want to swap that for your real domain everywhere before this goes live, since a fake domain in the canonical tag would actively hurt indexing.
