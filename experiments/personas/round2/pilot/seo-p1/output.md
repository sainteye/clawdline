Confirmed clean: `thanks.html`'s `noindex` is correct (it's a form-confirmation page that shouldn't be indexed), and every page now has exactly one canonical tag.

## Summary

Your bike shop's site had several technical SEO issues that were actively blocking Google from indexing your most important pages, in both languages. Fixed:

1. **`services.html` had `noindex, nofollow`** — your English prices page was explicitly hidden from Google. This was likely the single biggest hit to English visibility.
2. **`robots.txt` blocked `/blog/` entirely** — your blog posts (chain care, tyre pressure) are real, finished, keyword-relevant content that Google was never allowed to crawl.
3. **The "Shop" link on every page was a `<span onclick>`, not a real `<a href>`** — Googlebot can't follow JS-only links, so `/shop/` pages had almost no internal-link path to be discovered, in English or German.
4. **Dead link**: `/repairs.html` was referenced in the sitemap and two pages but doesn't exist — fixed to point to `/services.html`.
5. **Invalid hreflang code**: `contact.html`/`de/kontakt.html` used `hreflang="ge"` (not a real language code) instead of `de`, breaking the EN/DE pairing Google uses to serve the right language version.
6. **Missing reciprocal hreflang**: `de/leistungen.html` had no link back to the English `services.html`.
7. **Duplicate/conflicting canonical tags** on `shop/tubeless-kit.html`, and an `http://` canonical on `blog/tyre-pressure.html` (should be `https://`).

Also cleaned up the now-unused `.nav-link` CSS rule.

**Worth considering next** (didn't do, since they're bigger changes): adding `LocalBusiness` JSON-LD structured data (name/address/phone/hours) helps a lot with local "bike repair near me" results and Google Business Profile alignment, and there's currently no German blog content — if German search traffic matters, a couple of translated or German-original posts would help there too. Once these fixes are live, resubmit the sitemap in Google Search Console (and check the URL Inspection tool on `services.html` and a blog post) to speed up re-crawling.
