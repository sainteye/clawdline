import { catalogWord, currentCatalogTag } from "../../catalog.js"
import { RefusalError, asMachineNeedsUpdate } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import { workWord } from "./words.js"

/** A time on these routes (Unix seconds) as a person reads it: month, day, hour, minute. */
export function when(seconds: number | null | undefined): string {
  if (!seconds) return ""
  const tag = currentCatalogTag()
  const locale = tag === "zh-Hant" ? "zh-TW" : tag === "zh-Hans" ? "zh-CN" : tag
  return new Date(seconds * 1000).toLocaleString(locale, {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
}

/**
 * A failed command as a sentence. A refusal names what the daemon said; a
 * connection that never answered says that nothing is known to have been done,
 * which is not the same as "it failed" (refusal.ts).
 */
export function failureWords(e: unknown): string {
  // A machine older than the feature did not fail: it says it needs an
  // update, on every work-page surface that shows a failure (docs/updates.md).
  if (asMachineNeedsUpdate(e)) return nextWord("machineNeedsUpdate")
  if (e instanceof RefusalError) {
    if (e.code === "cloud_not_carried") return nextWord("cloudNotCarried")
    if (e.code === "version_conflict") return workWord("failedConflict")
    if (e.code === "images_full") return catalogWord("literal", "c93791de6596")
    if (e.code === "image_too_large" || e.code === "body_too_large") return catalogWord("literal", "5ee0423efcee")
    if (e.code === "unsupported_image") return catalogWord("literal", "b32c94186fd8")
    // `detail` is the daemon's English. The catalog has a sentence per code
    // and puts `code · ref` after it; the fallback is deliberately generic so
    // an unknown code is not repeated as both prose and tag.
    return L.failureSentence(e, workWord("failed"))
  }
  return workWord("failedNetwork")
}
