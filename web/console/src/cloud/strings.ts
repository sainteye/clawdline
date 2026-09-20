// The interface's words, on the hosted console.
//
// There is one catalog in this repository — `web/console/public/strings/<tag>.json`,
// the Swift app's, copied and kept byte for byte (docs/replica.md). Everything
// that says a word in this interface reads that one file, and the only
// difference between the transports is how it gets to the page:
//
//   - served by the daemon: it is written into the document
//     (`internal/transport/http/page.go`, the `clawdline:strings` slot), and
//     `GET /v1/strings` serves the same file for a page that has no slot;
//   - hosted for Clawdline Cloud: the bundle carries it, because `public/` is
//     part of the build, and this file reads it from the bundle's own origin.
//
// So the catalog cannot drift from the daemon's: there is nothing to drift
// from. What can drift is *which* catalog, and that is why `DEFAULT_TAG` is
// here rather than implied: the daemon answers `zh-Hant` to a request that
// names no language (`page.go`, `Server.strings`), and before this file a
// hosted page answered nothing at all unless the browser's own language
// happened to match an alias the build declared. The same person therefore
// read Chinese on the Mac and English on their phone, of the same machine, and
// `<html lang="en">` stayed as the document shipped it, which is what
// `applyStrings` reads the language back out of (`legacy/js/core/i18n.js`).
//
// The alias map is still honoured where the build declares one: it is how a
// build ships more than one catalog. It just no longer decides whether there
// is a catalog at all.

/**
 * The catalog a build serves when nothing narrows it — the daemon's own
 * default, and the tag `web/console/index.html` ships in `<html lang>`.
 *
 * `carry.test.ts` pins all three to this constant.
 */
export const DEFAULT_TAG = "zh-Hant"

/** The language tag the built-in words in `legacy/js/core/i18n.js` are written in. */
export const BUILTIN_TAG = "en"

/** The little of the build's Cloud declaration this file reads. */
export interface StringsConfig {
  /** The immutable build id the hosted bundle is served under, or "". */
  build: string
  /** `language prefix` → catalog tag, as the declaration spells it. */
  strings: Record<string, string>
}

/**
 * Where the catalog for `tag` is, in this build.
 *
 * With a build id, the hosted layout the declaration promises:
 * `/app/<build>/strings/<tag>.json` — the same address `cloudStringsURL`
 * computes in the copied `cloud-boot.js`, which is where this rule comes from.
 * The bundle's files are immutable under that id, so this is a URL that can be
 * cached forever and cannot answer a different build's words.
 *
 * Without one — a dev server, a copy opened from a checkout — it is resolved
 * against the document, which is where `public/` sits there.
 */
export function catalogURL(config: StringsConfig, tag: string, baseURI: string): string {
  const file = "strings/" + encodeURIComponent(tag) + ".json"
  if (config.build) return "/app/" + encodeURIComponent(config.build) + "/" + file
  return new URL(file, baseURI).toString()
}

/**
 * The catalog tag for this browser: the longest declared alias one of its
 * languages starts with, else the build's default.
 *
 * The longest alias wins so that `zh-Hant` is preferred over `zh` for a
 * browser asking for `zh-Hant-TW`, which is the rule `cloudStringsURL` already
 * used and the reason the aliases are sorted rather than iterated as written.
 */
export function catalogTag(config: StringsConfig, languages: readonly string[]): string {
  const aliases = Object.keys(config.strings).sort((a, b) => b.length - a.length)
  for (const language of languages) {
    const alias = aliases.find((a) => language.toLowerCase().startsWith(a.toLowerCase()))
    if (alias) return config.strings[alias]
  }
  return DEFAULT_TAG
}

/**
 * The words, read from this build's own files.
 *
 * A catalog that cannot be read throws, and the caller keeps the built-in
 * English and says so in the document's `lang` — a console that refused to
 * start over a translation would be worse than one that starts in English, and
 * a page claiming a language it is not showing is worse than both.
 */
export async function bundledCatalog(
  config: StringsConfig,
  languages: readonly string[],
  baseURI: string,
  get: typeof fetch = fetch,
): Promise<Record<string, string>> {
  const tag = catalogTag(config, languages)
  const res = await get(catalogURL(config, tag, baseURI))
  if (!res.ok) throw new Error("catalog " + tag + ": " + res.status)
  return (await res.json()) as Record<string, string>
}
