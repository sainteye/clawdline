/*
 * Whether the page somebody is looking at is the build being served.
 *
 * **A page here can be days old.** Added to a Home Screen, a console is opened
 * and never navigated again: its modules are the ones it loaded, whenever that
 * was. The Mac it talks to is rebuilt several times a day. So the two drift,
 * and the page has no way to find out — the drift shows up as its presses
 * quietly not working.
 *
 * That is not a hypothetical. On 2026-09-20 a daemon gained a rule that a menu
 * answer must name the question it was chosen for; a page old enough to send
 * neither that name nor a request id had every press refused, and the refusal
 * had no channel back to a page that old, so two taps on a question went
 * nowhere and said nothing. The refusal's own words were "Reload the page and
 * answer again", which nobody could read.
 *
 * The build is named by its files: the index lists the entry modules and their
 * styles, each with a hash of its contents in the name. So the page asks the
 * index what it names now and looks for each of those among the files it
 * loaded. **One the page has not got is a build the page is not running.**
 *
 * The question is deliberately one-directional, and the first version of this
 * file got that wrong: it asked whether the two sets were equal, which made
 * every hosted console say it was out of date, every time, from its first
 * minute. `main.tsx` reaches the Cloud gate through a dynamic `import()`, so
 * `CloudGate-*.js` and its stylesheet are named in no index and are in the
 * page's own tags the moment the gate loads. Measured on 2026-09-21: the index
 * names six assets, the bundle holds twelve, and a page at the gate carries
 * eight — so "the same number of files" was never once true in production.
 * A page loading more than the index names is the normal shape of code
 * splitting, not evidence of anything.
 *
 * That leaves the opposite worry — a rebuild the looser question misses,
 * because the index still names files the page happens to hold. It does not
 * happen, and the reason is mechanical: a chunk's file name is a literal
 * string inside whichever module imports it, so a chunk's hash moving moves
 * its importer's hash too, up to the entry the index names. Measured with two
 * real builds on the same day: one string changed inside `CloudGate.tsx` alone
 * renamed `CloudGate-C87cOfFs.js` to `CloudGate-SlWRtfMJ.js` **and**
 * `main-CBCqbkb3.js` to `main-BOMcW7yy.js`, which the index names.
 *
 * **It never guesses in the direction of alarm.** An index it cannot read
 * anything out of, or a page whose own assets it cannot see, is "no answer",
 * not "out of date": an offer to reload that appears for no reason teaches
 * people to ignore it, and the one that matters would be ignored with it.
 * This is also why the question is not asked the other way round: "the page
 * holds a file the index does not name" is true of every code-split page and
 * would be exactly such an alarm.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */

/**
 * The hashed asset paths in `text`, sorted and without repeats.
 *
 * Matched as paths rather than by parsing tags, because the same path is a
 * `<script src>`, a `<link rel=modulepreload href>` and a stylesheet's `href`,
 * and what identifies the build is the set of files, not which tag names them.
 */
export function assetsNamed(text: string): string[] {
  const found = new Set<string>()
  for (const match of text.matchAll(/(assets\/[A-Za-z0-9._-]+\.(?:js|css))/g)) found.add(match[1])
  return [...found].sort()
}

/** The same, from URLs a page already loaded (`script[src]`, `link[href]`). */
export function assetsLoaded(urls: readonly string[]): string[] {
  return assetsNamed(urls.join("\n"))
}

/**
 * Whether the index at `served` names a build this page is not running.
 *
 * True when the index names an asset the page has not loaded. Assets the page
 * loaded and the index does not name are the dynamic chunks it reached on its
 * way here, and say nothing — see the note above.
 *
 * False whenever there is nothing to compare — see the note above about never
 * guessing towards alarm.
 */
export function isDifferentBuild(served: string, running: readonly string[]): boolean {
  const there = assetsNamed(served)
  const here = new Set(assetsLoaded(running))
  if (there.length === 0 || here.size === 0) return false
  return there.some((path) => !here.has(path))
}
