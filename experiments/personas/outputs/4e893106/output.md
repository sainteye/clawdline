Both fit comfortably within typical search-result display limits (title 60 chars, description 153 chars).

## What I changed in `posts/seasoning-cast-iron.html`

This page was missing nearly every on-page SEO signal the site's other pages already have (compare `about.html` / `cast-iron-cornbread.html`):

- **Title**: was just `Iron & Ember` (identical to the homepage, tells search engines nothing) → now `How to Season a Cast Iron Skillet in the Oven | Iron & Ember` (60 chars, unique, matches instructional search intent the page genuinely covers).
- **Meta description**: none → added a 153-char summary reflecting the actual content (the $8 yard-sale skillet, oven method, temp, fix for a sticky first attempt).
- **Canonical link**: none → added, pointing at the URL already listed in `sitemap.xml`.
- **`lang="en"`**: added to `<html>`, matching every other page on the site.
- **Heading structure**: the title was a styled `<div class="post-title">`, not a heading element, so the page had no `<h1>` and its `<h2>` steps were structurally orphaned. Replaced with a real `<h1>`, matching the pattern already used on `cast-iron-cornbread.html`.
- **Image alt text**: both step images (`skillet-oiled.png`, `skillet-oven.png`) had no `alt` attribute at all. Added descriptive alt text based on what each step's surrounding text says the image shows.
- **Link text**: "click here" → "skillet cornbread with brown butter", so the link (and the page it points to) has meaningful anchor text instead of a generic phrase.

I verified by reading the rendered file back and confirming it now matches the conventions already established on `about.html` and `cast-iron-cornbread.html`, and that the canonical URL matches the existing `sitemap.xml` entry.

**Left as-is / worth a decision from you:**
- The `.post-title` CSS rule in `css/site.css` is now unused since I switched to a plain `<h1>` (matching the rest of the site). Harmless, but you may want to delete it.
- Images are rendered at a fixed `width="64" height="48"` site-wide — fine for layout, but if these are meant to be meaningfully sized content photos rather than thumbnails, that's a site-wide template decision beyond this one article.
