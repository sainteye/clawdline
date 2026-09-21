import type { BearingsSource, ScanSource, SessionRow } from "@clawdline/contract"

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

/**
 * The concrete reason a terminal source did not finish, translated at the UI
 * boundary. Adapter diagnostics still cross the wire for evidence, but their
 * English subprocess text is never used as the sentence on a Chinese screen.
 */
export function scanFailureWords(
  notes: readonly string[] | undefined,
  sources: readonly ScanSource[] | undefined,
  chinese = sessionReadingChinese(),
): string | null {
  for (const note of notes ?? []) {
    if (note.startsWith("iTerm2 apple event failed:")) {
      return chinese
        ? "Clawdline 讀不到 iTerm2。請到「系統設定 → 隱私權與安全性 → 自動化」，允許 Clawdline 控制 iTerm2。"
        : "Clawdline could not read iTerm2. In System Settings → Privacy & Security → Automation, allow Clawdline to control iTerm2."
    }
    if (note.startsWith("iTerm2 answer was unreadable:")) {
      return chinese
        ? "iTerm2 回傳的 session 清單無法讀取；請重新整理，若仍然發生，再重新啟動 iTerm2。"
        : "iTerm2 returned an unreadable session list. Refresh it, and restart iTerm2 if it continues."
    }
    const outside = /^tmux is at (.+), which is not on this daemon's PATH/.exec(note)
    if (outside) {
      return chinese
        ? `Clawdline 在 ${outside[1]} 找到 tmux，但 daemon 的 PATH 沒有它；這次無法確認 tmux 的 session 清單。`
        : `Clawdline found tmux at ${outside[1]}, but it is not on the daemon's PATH, so this pass could not verify the tmux session list.`
    }
    if (note.startsWith("tmux list-panes failed:")) {
      return chinese
        ? "tmux 沒有完成 session 清單讀取；請在這台機器執行 `tmux list-panes -a` 查看它拒絕的原因。"
        : "tmux did not finish reading its session list. Run `tmux list-panes -a` on this machine to see why it refused."
    }
  }

  const openGap = (sources ?? []).flatMap((source) => source.gaps ?? []).find((gap) => !gap.sealed)
  if (openGap) {
    return chinese
      ? "iTerm2 有一個視窗或分頁沒有回報 session；請打開 iTerm2，檢查沒有內容或正在等待回應的視窗。"
      : "An iTerm2 window or tab did not report its sessions. Open iTerm2 and check for a blank or unresponsive window."
  }
  const incomplete = (sources ?? []).find((source) => !source.complete)
  if (incomplete) {
    return chinese
      ? `${incomplete.source} 沒有完成 session 清單讀取；空白不代表沒有 session。`
      : `${incomplete.source} did not finish reading its session list; a blank list does not mean there are no sessions.`
  }
  return null
}

/** The header's first number is always the list's total, never one state. */
export function totalSessionWords(total: number, chinese = sessionReadingChinese()): string {
  if (total === 0) return chinese ? "沒有 session" : "no sessions"
  if (chinese) return `${total} 個 session`
  return `${total} ${total === 1 ? "session" : "sessions"}`
}
