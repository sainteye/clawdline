import type * as W from "@clawdline/contract"
import type { Words } from "./words"

// Always K/M, as the context meter reads: zh-Hant's own compact form would read 34.5萬.
const compact = new Intl.NumberFormat("en", {notation: "compact", maximumFractionDigits: 1})

// A notice Clawdline wrote is rendered from its key in the reader's language.
// Notices stored before the key existed carried both languages, or English
// alone, ending in "345K → 41K"; they read the same way from their ID and sizes.
// Anything else is shown as stored.
export function compactedNoticeText(message: Pick<W.ProjectAssistantMessage, "id" | "kind" | "text" | "notice">, w: Words): string | null {
  if (message.kind !== "notice") return null
  const ref = message.notice
  if (ref?.key === "compacted") {
    const before = ref.tokens_before ?? 0, after = ref.tokens_after ?? 0
    return before > 0 && after > 0 && after < before
      ? w.compactedNoticeSizes.replace("{before}", compact.format(before)).replace("{after}", compact.format(after))
      : w.compactedNotice
  }
  if (ref || !message.id.startsWith("compacted-") || !message.text.startsWith("Earlier context was summarized to make room")) return null
  const sizes = /([0-9][0-9.]*[KM]?) → ([0-9][0-9.]*[KM]?)$/.exec(message.text)
  return sizes ? w.compactedNoticeSizes.replace("{before}", sizes[1]!).replace("{after}", sizes[2]!) : w.compactedNotice
}
