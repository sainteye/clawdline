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

/** The locale choice used by the session shell, without adding catalog keys. */
export function sessionReadingChinese(): boolean {
  const lang =
    (typeof document !== "undefined" ? document.documentElement.lang : "") ||
    (typeof navigator !== "undefined" ? navigator.language : "") ||
    ""
  return lang.toLowerCase().startsWith("zh")
}

/** A retained row is an earlier fact, said with its age every time. */
export function retainedStateWords(
  row: Pick<SessionRow, "source" | "state">,
  nowSeconds = Date.now() / 1000,
  chinese = sessionReadingChinese(),
): string | null {
  if (row.source?.freshness !== "unverified") return null
  const age = ageWords(Math.max(0, nowSeconds - row.source.observed_at), chinese)
  if (chinese) {
    if (row.state === "working") return `${age}前在跑`
    if (row.state === "waiting") return `${age}前在等你回答`
    if (row.state === "idle") return `${age}前很安靜`
    return `${age}前的狀態未知`
  }
  if (row.state === "working") return `working ${age} ago`
  if (row.state === "waiting") return `waiting for you ${age} ago`
  if (row.state === "idle") return `quiet ${age} ago`
  return `state unknown ${age} ago`
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
