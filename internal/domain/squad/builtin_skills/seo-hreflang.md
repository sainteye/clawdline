# Hreflang and multilingual pages

Use this skill when a site serves several languages or regions and its hreflang annotations, language codes or translated pages must be audited or generated; not for single-language sites or for translating content.

1. Map every page to its equivalents in each language or region, from URL paths, subdomains, domains and the `lang` attribute, using the files or sitemap given.
2. Check each set: every page lists itself and every alternate; every link is returned by the page it points to; at most one `x-default`, used where a fallback or selector page exists and returned by the others; codes are an ISO 639-1 language, optionally an ISO 15924 script (`zh-Hant`, `zh-Hans`) and an ISO 3166-1 region (`en-GB`, not `en-uk`; never a region alone or a non-country such as `EU`); every hreflang URL is the canonical URL exactly, with the same protocol, host and trailing slash, and non-canonical pages appear in no set; cross-domain sets have return links on both domains.
3. Pick one method per set: link tags in the head for few variants, HTTP `Link` headers for non-HTML files, or a sitemap with the `xhtml` namespace in which every `<url>` lists all alternates including itself, for many variants or several domains. Do not mix methods for the same pages.
4. Hreflang is a hint, not a directive. Do not present a ranking order of locale signals as confirmed, and do not suggest search-console country-targeting settings that no longer exist.
5. Check parity across versions: the equivalent page exists; section structure, images, structured data, title and meta description are localized; translations older than their source's last change are flagged as possibly stale; numbers, dates, currency and phone formats follow the locale (use the project's locale library or CLDR data); no untranslated navigation or wrong-jurisdiction legal text. Flag machine-translated pages with no sign of human review as a quality and spam-policy risk, and check the search engine's current policy before stating it.

Report a table of each URL with self-reference, return links, x-default and status, then corrected tags or a sitemap, then parity findings with the evidence for each. Do not score cultures or expect fixed word-count ratios. Fetch remote pages only when the task allows network access.

Adapted from AgriciDaniel/claude-seo's `seo-hreflang` skill and its four references at commit 6be93f45064ffbadced572b5d5c95f55fd263af0 (MIT; copyright 2026 agricidaniel). See the pinned source and bundled license in the skill catalog.
