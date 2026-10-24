// @ts-expect-error Node's strip-types test runner needs the source extension.
import { filterHash, filtersFromHash } from "./model.ts"
/** Public documentation needs no Cloud account or paired machine. */
export function transparencyURL(language: string, hash = ""): string {
  const url = new URL("https://clawdline.com/docs/transparency/")
  url.searchParams.set("lang", language.startsWith("zh-Hant") || language === "zh-TW" ? "zh-TW" : "en")
  const filters = new URLSearchParams(filterHash(filtersFromHash(hash)).slice(1))
  filters.delete("page")
  if (filters.size) url.hash = filters.toString()
  return url.href
}
