// The hosted console reads the same new catalogs that the local daemon serves.
// The copied Swift catalog stays untouched under public/strings/; this bundle
// carries the maintained files under public/catalogs/.

/**
 * English is the document's initial and final fallback language.
 */
import english from "../../public/catalogs/en.json" with { type: "json" }
// @ts-expect-error -- Node runs carry.test.ts against this source file directly.
import { browserPreference, chooseTag, DEFAULT_TAG, validCatalog, type CatalogTag } from "../catalog.ts"

export { DEFAULT_TAG }

/** The language tag the built-in words in `legacy/js/core/i18n.js` are written in. */
export const BUILTIN_TAG = "en"

/** The little of the build's Cloud declaration this file reads. */
export interface StringsConfig {
  /** The immutable build id the hosted bundle is served under, or "". */
  build: string
  /** Legacy declaration aliases; interface selection now uses catalog.ts. */
  strings: Record<string, string>
}

/**
 * Where the catalog for `tag` is, in this build.
 *
 * With a build id, the hosted layout the declaration promises:
 * `/app/<build>/catalogs/<tag>.json`.
 * The bundle's files are immutable under that id, so this is a URL that can be
 * cached forever and cannot answer a different build's words.
 *
 * Without one — a dev server, a copy opened from a checkout — it is resolved
 * against the document, which is where `public/` sits there.
 */
export function catalogURL(config: StringsConfig, tag: string, baseURI: string): string {
  const file = "catalogs/" + encodeURIComponent(tag) + ".json"
  if (config.build) return "/app/" + encodeURIComponent(config.build) + "/" + file
  return new URL(file, baseURI).toString()
}

/**
 * The same browser-local choice and script-aware resolver as the local page.
 */
export function catalogTag(config: StringsConfig, languages: readonly string[]): CatalogTag {
  void config
  return chooseTag(browserPreference(), languages)
}

/**
 * The words, read from this build's own files.
 *
 * The relay's /v1/strings seam uses the same atomic fallback as the page boot.
 * Valid secondary catalogs may omit keys added after the initial baseline.
 */
export async function bundledCatalog(
  config: StringsConfig,
  languages: readonly string[],
  baseURI: string,
  get: typeof fetch = fetch,
): Promise<Record<string, string>> {
  const tag = catalogTag(config, languages)
  if (tag === "en") return english
  try {
    const res = await get(catalogURL(config, tag, baseURI))
    if (!res.ok) return english
    const candidate: unknown = await res.json()
    return validCatalog(candidate, tag) ? candidate : english
  } catch {
    return english
  }
}
