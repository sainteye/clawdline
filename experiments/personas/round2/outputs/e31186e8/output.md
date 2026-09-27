All three EN/DE page pairs (home, contact/kontakt, services/leistungen) now correctly reference each other with valid `de`/`en` codes and reciprocal links. That covers everything I found. Here's the summary.

## What I fixed

**Blocking indexing outright**
- `robots.txt` had `Disallow: /blog/` — blocking your two finished, dated blog articles from Google entirely, even though they're linked from the homepage and in the sitemap. Removed.
- `services.html` (English prices page) had `<meta name="robots" content="noindex, nofollow">` — hiding your English pricing page from Google while the German version wasn't hidden. Removed.
- **`de/index.html`'s canonical tag pointed to the English homepage** instead of itself — telling Google "index the English page instead of this one." This alone could explain most of your German-visibility problem. Fixed to self-reference `/de/`.

**Crawlability**
- The "Shop" nav link was a `<span onclick="location.href=...">`, not a real `<a href>`, on all 12 pages — search engines don't reliably follow that, so `/shop/` had no crawlable internal links. Replaced with real anchors everywhere.
- Two internal links pointed to `/repairs.html`, which doesn't exist (real page is `/services.html`) — in the blog article and the shop product page.
- `sitemap.xml` also listed the nonexistent `/repairs.html`, and listed `/thanks.html` (a noindex form-confirmation page that shouldn't be submitted for indexing). Both removed.

**Duplicate/incorrect metadata**
- All three blog pages (`/blog/`, the two articles) shared the identical `<title>` and meta description. Gave each article its own, matching its actual content.
- `hreflang="ge"` (invalid code) on `contact.html` and `de/kontakt.html` → fixed to `de`.
- `de/leistungen.html` was missing its reciprocal `hreflang="en"` back to `services.html`.
- `shop/tubeless-kit.html` had two conflicting canonical tags (one pointing at `/shop/`, one at itself). Removed the wrong one.
- `blog/tyre-pressure.html`'s canonical used `http://` instead of `https://`.

**Added**
- `BicycleStore` JSON-LD structured data on both homepages (name, address, phone, hours) — all facts already visible on the page — to support local/Maps search.

## Verified
Re-read every edited file, confirmed no leftover `onclick`, `/repairs.html`, or `hreflang="ge"` references, checked all 12 sitemap URLs resolve to real files, and re-checked every canonical/hreflang pair site-wide for self-consistency.

## Left as proposals (need your call, not code fixes)
- **German content depth**: the blog only exists in English. Translating the two existing articles into German would give you German-language long-tail content — I can do this if you want, but it's a real content task, not a quick fix, so I didn't do it unprompted.
- **Server-level checks I can't verify from these files alone**: whether `http://` actually redirects to `https://`, and www vs non-www canonicalization — that's hosting/DNS config outside this repo.
- Two shop items ("Chain wax", "Resin brake pads") have no product page of their own, so they can't rank for their own searches — worth a page each if you want that traffic.
