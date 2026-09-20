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
 * index what it names now and compares that with what it loaded. A different
 * set is a different build.
 *
 * **It never guesses in the direction of alarm.** An index it cannot read
 * anything out of, or a page whose own assets it cannot see, is "no answer",
 * not "out of date": an offer to reload that appears for no reason teaches
 * people to ignore it, and the one that matters would be ignored with it.
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
 * Whether the index at `served` names a different build from `running`.
 *
 * False whenever there is nothing to compare — see the note above about never
 * guessing towards alarm.
 */
export function isDifferentBuild(served: string, running: readonly string[]): boolean {
  const there = assetsNamed(served)
  const here = assetsLoaded(running)
  if (there.length === 0 || here.length === 0) return false
  return there.length !== here.length || there.some((path, i) => path !== here[i])
}
