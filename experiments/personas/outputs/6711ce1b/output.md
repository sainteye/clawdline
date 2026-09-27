All referenced files exist. Here's what changed in `posts/seasoning-cast-iron.html`, bringing it in line with the rest of the site (`cast-iron-cornbread.html`, `about.html`, `index.html` already followed this pattern):

**Before → after**
- `<title>Iron & Ember</title>` (same as homepage, not descriptive) → `How to Season a Cast Iron Skillet in the Oven | Iron & Ember` (60 chars, unique, matches the query someone would type)
- No meta description → added a 139-char description summarizing the actual steps (oil, 450°F, troubleshooting)
- No canonical tag → added, pointing to the URL already listed in `sitemap.xml`
- No `lang` attribute on `<html>` → added `lang="en"`
- `<div class="post-title">` instead of a real heading → converted to `<h1>`, so the page has exactly one, matching its title
- Both step images had no `alt` text → added descriptive alt text based on what each step's surrounding paragraph says is happening
- Internal link to the cornbread post used generic anchor text "click here" → changed to "skillet cornbread with brown butter" (the target page's actual title), which helps both users and crawlers understand the destination

Also removed the now-unused `.post-title` CSS rule from `css/site.css` since nothing references it after the `<h1>` conversion.

**Verified:** no build step exists (pure static HTML), so the edited file is the served output; confirmed the canonical URL matches the existing sitemap entry, and all linked assets/pages resolve to real files.

**Not touched / left as-is:** `sitemap.xml` and `robots.txt` were already correct for this page. `index.html`'s link text to this post was already descriptive. I didn't touch page copy beyond the one anchor-text fix — the voice/content is otherwise fine and I didn't want to reshape it around keywords it doesn't need.
