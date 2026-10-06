import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import { workWord } from "./words.js"

/** A time on these routes (Unix seconds) as a person reads it: month, day, hour, minute. */
export function when(seconds: number | null | undefined): string {
  if (!seconds) return ""
  const zh = (document.documentElement.lang || navigator.language || "").toLowerCase().startsWith("zh")
  return new Date(seconds * 1000).toLocaleString(zh ? "zh-TW" : "en", {
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
  if (e instanceof RefusalError) {
    if (e.code === "cloud_not_carried") return nextWord("cloudNotCarried")
    if (e.code === "version_conflict") return workWord("failedConflict")
    if (e.code === "images_full") return "每個項目或 Session 待辦最多可放 6 張參考圖片。"
    if (e.code === "image_too_large" || e.code === "body_too_large") return "圖片太大，請縮小後再試。"
    if (e.code === "unsupported_image") return "這個檔案不是可讀取的圖片。"
    // `detail` is the daemon's English. The catalog has a sentence per code
    // and puts `code · ref` after it; the fallback is deliberately generic so
    // an unknown code is not repeated as both prose and tag.
    return L.failureSentence(e, workWord("failed"))
  }
  return workWord("failedNetwork")
}
