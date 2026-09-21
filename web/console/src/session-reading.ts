import type { BearingsSource, SessionRow } from "@clawdline/contract"

function ageWords(seconds: number, chinese: boolean): string {
  const s = Math.max(0, Math.floor(seconds))
  const days = Math.floor(s / 86400)
  if (days >= 1) return chinese ? `${days} 天` : `${days}d`
  const hours = Math.floor(s / 3600)
  if (hours >= 1) return chinese ? `${hours} 小時` : `${hours}h`
  const minutes = Math.floor(s / 60)
  if (minutes >= 1) return chinese ? `${minutes} 分鐘` : `${minutes}m`
  return chinese ? `${s} 秒` : `${s}s`
}

// The list normally reads again every ten seconds (`Sessions.tsx`). One missed
// pass is ordinary repaint timing, not a warning worth adding to every row.
// Three missed passes are the first point where the age becomes useful context.
const retainedAgeNoteAfterSeconds = 30

/** The locale choice used by the session shell, without adding catalog keys. */
export function sessionReadingChinese(): boolean {
  const lang =
    (typeof document !== "undefined" ? document.documentElement.lang : "") ||
    (typeof navigator !== "undefined" ? navigator.language : "") ||
    ""
  return lang.toLowerCase().startsWith("zh")
}

/** An older retained row is an earlier fact, said quietly with its age. */
export function retainedStateWords(
  row: Pick<SessionRow, "source" | "state">,
  nowSeconds = Date.now() / 1000,
  chinese = sessionReadingChinese(),
): string | null {
  if (row.source?.freshness !== "unverified") return null
  const ageSeconds = Math.max(0, nowSeconds - row.source.observed_at)
  if (ageSeconds < retainedAgeNoteAfterSeconds) return null
  const age = ageWords(ageSeconds, chinese)
  if (chinese) {
    if (row.state === "working") return `${age}前在跑`
    if (row.state === "waiting") return `${age}前在等你回答`
    if (row.state === "idle") return `${age}前沒有新輸出`
    return `${age}前讀到`
  }
  if (row.state === "working") return `working ${age} ago`
  if (row.state === "waiting") return `waiting for you ${age} ago`
  if (row.state === "idle") return `no new output ${age} ago`
  return `read ${age} ago`
}

/** One batch failure belongs above the list, not repeated as six row failures. */
export function batchReadingWords(
  source: BearingsSource | undefined,
  nowSeconds = Date.now() / 1000,
  chinese = sessionReadingChinese(),
): string | null {
  if (!source || source.freshness === "current") return null
  if (source.freshness === "unverified") {
    const age = ageWords(Math.max(0, nowSeconds - source.observed_at), chinese)
    return chinese
      ? `這次沒有讀完整；其中保留的內容最後在 ${age}前讀到。`
      : `This pass did not finish; retained parts were last read ${age} ago.`
  }
  return chinese
    ? "這次沒有讀完整；至少一個來源沒有仍可採用的上次讀數。"
    : "This pass did not finish; at least one source has no recent earlier reading to show."
}

/** The header's first number is always the list's total, never one state. */
export function totalSessionWords(total: number, chinese = sessionReadingChinese()): string {
  if (total === 0) return chinese ? "沒有 session" : "no sessions"
  if (chinese) return `${total} 個 session`
  return `${total} ${total === 1 ? "session" : "sessions"}`
}
