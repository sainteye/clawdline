This is well-formed JSON-LD. All fixes are in place. Summary of what changed:

**Fixed (all directly in the code):**
1. `robots.txt` — removed `Disallow: /blog/`, so the blog can be crawled and indexed in both languages.
2. `services.html` — removed `noindex, nofollow`; your prices page can now be indexed and pass link value.
3. All 12 pages — replaced the JS-only `<span onclick>` "Shop" link with a real `<a href="/shop/">`, so crawlers can discover and follow it (also dropped the now-unused `.nav-link` CSS rule).
4. `blog/winter-chain-care.html` and `shop/tubeless-kit.html` — fixed dead links to `/repairs.html` → `/services.html`.
5. `sitemap.xml` — fixed that same broken URL and removed `/thanks.html` (a noindex confirmation page that shouldn't be submitted).
6. `contact.html` and `de/kontakt.html` — fixed `hreflang="ge"` (invalid) → `"de"`, and added missing `x-default` tags.
7. `de/leistungen.html` — added the missing reciprocal `hreflang="en"`/`x-default` back to `services.html`.
8. `blog/tyre-pressure.html` — canonical URL fixed from `http://` to `https://`.
9. `shop/tubeless-kit.html` — removed a duplicate, conflicting canonical tag that pointed at the shop index instead of itself.
10. Both blog articles — replaced the identical generic "Blog | Werkbank Cycles" title/description with unique, content-specific ones.
11. `blog/winter-chain-care-print.html` — added `noindex` (near-duplicate of the main article, consistent with how `thanks.html` is already handled).
12. Added `BicycleStore` JSON-LD structured data (name, address, phone, hours) to both homepages — helps Google's local pack/maps results for "bike repair Berlin," using only facts already published on the page.

**Worth knowing but not something I changed:**
- The blog only exists in English (`de/kontakt.html` even labels it "Blog (Englisch)"). That's a content decision, not a bug — if ranking in German search matters, translating or writing German posts would help, but that's new content the shop should approve, not something to fabricate.
- Consider adding a free Google Business Profile listing tied to the same address/phone/hours — that's usually the single biggest lever for local "bike shop near me" visibility, but it's an account action outside this codebase.

Once these are live, use Google Search Console for both properties to request re-indexing of `/services.html` and the `/blog/` pages so Google picks up the changes faster.
