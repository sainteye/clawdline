All three hreflang pairs (home, services/Leistungen, contact/Kontakt) are now fully reciprocal and consistent in both directions. Everything checks out.

## Summary

Your site had several things actively telling Google *not* to show it — this wasn't really a "not found" problem, it was a "told not to look" problem:

**Fixed:**
- **`services.html` had `noindex, nofollow`** — your pricing page, arguably your most important page, was invisible to Google and blocked from passing links to `/contact.html`. Removed.
- **`de/index.html`'s canonical tag pointed at the English homepage** instead of itself — this told Google the German homepage wasn't a real, distinct page, which would suppress it from German results specifically. Fixed to self-reference `/de/`.
- **`robots.txt` blocked `/blog/` entirely** while the sitemap and homepage linked to real posts inside it. Unblocked.
- **The "Shop" link was JavaScript (`onclick`), not a real `<a href>`**, on all 12 pages — crawlers can miss those, so `/shop/` and its product page were poorly discoverable. Converted to real links everywhere.
- **Broken EN↔DE hreflang**: `contact.html`/`de/kontakt.html` used the invalid code `ge` instead of `de`; `de/leistungen.html` didn't link back to `services.html` at all. Both pairs are now correct and reciprocal, with `x-default` added for consistency.
- Cleanup: duplicate/contradictory canonical tag on the shop product page, an `http://` canonical on one blog post, three dead links to a nonexistent `/repairs.html`, and a noindexed page that was listed in the sitemap.
- **Added `BicycleStore` structured data** (name, address, phone, hours) to both homepages, using only facts already published on the site — this helps Google show your address/hours directly in results and improves local-search matching.

**Not code fixes, but worth knowing:** a lot of "found on Google for a local shop" comes from outside the HTML — a claimed, complete **Google Business Profile** (with the exact same NAP — name/address/phone — as the site) and a few genuine customer reviews there typically move the needle for local search more than anything on-page. I didn't touch that since it's an account action, not a file change.

One thing to flag before you publish: all URLs on the site use the placeholder domain `werkbankcycles.example` — make sure your real domain is swapped in everywhere (canonical, hreflang, sitemap, robots.txt) once you deploy, or these tags will point at the wrong place.
