All clean — the only remaining match is the now-harmless leftover CSS rule. Here's what was wrong and what I fixed:

**Critical (these alone likely explain "barely shows up")**
1. **`services.html` had `<meta name="robots" content="noindex, nofollow">`** — your prices/services page, probably the single page most worth ranking for "bike repair Berlin Kreuzberg," was explicitly telling Google not to index it or follow its links. Removed.
2. **`robots.txt` disallowed `/blog/` entirely** — your two blog posts (good long-tail content like "tyre pressure city riding") were fully blocked from crawling, even though they were linked and in the sitemap. Removed the disallow (the comment said it was to hide "unfinished" posts, but finished posts already exist).

**Broken links / crawl issues**
3. Three places linked to `/repairs.html`, a page that never existed — the real page is `services.html`. Fixed in the sitemap and two content pages.
4. `shop/tubeless-kit.html` had **two conflicting `<link rel="canonical">` tags** (one pointing at `/shop/`, one at itself). Multiple canonicals make Google ignore both; removed the wrong one.
5. Every page's "Shop" nav item was `<span onclick="location.href=...">` instead of a real `<a href>` — search engines don't reliably follow onclick handlers, so there was effectively no crawlable link to your shop section from site navigation, anywhere. Fixed on all 12 pages.
6. `sitemap.xml` listed `thanks.html` (a noindex confirmation page) — pointless and can generate Search Console warnings. Removed.

**Language targeting (affects both English and German visibility)**
7. `contact.html` and `de/kontakt.html` both used `hreflang="ge"` — not a real language code (German is `de`; `ge` isn't assigned). Invalid hreflang values get ignored, so Google had no reliable EN↔DE link between your two contact pages. Fixed.
8. `blog/tyre-pressure.html`'s canonical URL used `http://` instead of `https://`. Fixed.

**Duplicate content**
9. Both blog posts had copy-pasted the blog index's generic `<title>Blog | Werkbank Cycles</title>` and description, so three different pages competed with identical title tags. Gave each post a unique, keyword-relevant title/description.

One thing I can't fix from code: for a local shop, a lot of "showing up on Google" comes from your **Google Business Profile** (the map pack / local results), not just the website — reviews, correct hours/address there, and photos matter as much as these fixes. Worth checking if that profile is claimed and up to date.

None of this is pushed anywhere (no git repo here) — just local file edits. Let me know if you want me to also add local-business structured data (JSON-LD) to the homepage, which helps Google show your address/hours/phone directly in search results.
