Everything's fixed. Here's what was actually keeping the site out of Google:

**Critical (these were almost certainly the main causes):**
1. **`services.html` had `<meta name="robots" content="noindex, nofollow">`** — your main prices/services page, probably your best commercial-intent page, was explicitly telling Google not to index it and not to follow its links. Removed.
2. **The German homepage's canonical tag pointed to the English homepage** (`de/index.html` canonicalized to `/` instead of `/de/`). This told Google "this page is a duplicate, index the English one instead" — likely the direct reason the German site wasn't showing up. Fixed to self-reference `/de/`.
3. **`robots.txt` blocked `/blog/`** entirely, even though it's fully written, linked from the homepage, and listed in the sitemap. The comment said "unfinished blog" but the posts are done. Removed the block.

**Real bugs also fixed:**
- `hreflang="ge"` (invalid code) instead of `"de"` on both contact pages, which broke the EN/DE language pairing there.
- Sitemap and two pages linked to `/repairs.html`, which doesn't exist — a 404 for users and crawlers. Fixed to `/services.html`.
- `shop/tubeless-kit.html` had two conflicting canonical tags. Removed the wrong one.
- `blog/tyre-pressure.html`'s canonical used `http://` instead of `https://`.
- Both blog posts shared the generic title/description "Blog | Werkbank Cycles" instead of unique, keyword-relevant ones — gave each a real title and description.
- The "Shop" nav item was a `<span onclick="...">` on every single page, not a real link — invisible to Google's crawler and to keyboard/screen-reader users. Converted to `<a href="/shop/">` sitewide.
- Removed `thanks.html` (a noindexed confirmation page) from the sitemap.

**Added:**
- `BicycleStore` JSON-LD structured data (address, phone, hours) on both homepages to help Google understand the business for local/map results.

Next step: once these are live, resubmit `sitemap.xml` in Google Search Console and use "Request Indexing" on the homepage and `/de/` — that should get the fixes crawled quickly rather than waiting for a natural recrawl.
