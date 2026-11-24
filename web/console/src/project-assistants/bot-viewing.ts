// Tells this machine which Bot chat is on screen, so a question the person can
// already see is not also pushed to their phone. The daemon counts one report
// for 45 seconds (orchestrator.BotViewingSeconds); this repeats well inside it
// while the chat is visible and focused, and says "left" as soon as it is not.
// A lost report only means a push is sent, never that one is missed.

export const BOT_VIEWING_REPEAT_MS = 20_000

export type ViewingReport = (bot: string, conversation: string, viewing: boolean) => void

export interface ViewingPage {
  visible(): boolean
  focused(): boolean
  listen(change: () => void): () => void
  every(ms: number, tick: () => void): () => void
}

/** Starts reporting one chat; the returned function stops and reports "left". */
export function watchBotViewing(bot: string, conversation: string, page: ViewingPage, report: ViewingReport): () => void {
  if (!bot || !conversation) return () => {}
  let on = false
  const look = () => {
    const now = page.visible() && page.focused()
    if (now) report(bot, conversation, true)
    else if (on) report(bot, conversation, false)
    on = now
  }
  look()
  const stopListening = page.listen(look)
  const stopTicking = page.every(BOT_VIEWING_REPEAT_MS, () => { if (on) report(bot, conversation, true) })
  return () => {
    stopListening()
    stopTicking()
    if (on) report(bot, conversation, false)
    on = false
  }
}

export function browserViewingPage(): ViewingPage {
  return {
    visible: () => document.visibilityState === "visible",
    focused: () => document.hasFocus(),
    listen(change) {
      document.addEventListener("visibilitychange", change)
      window.addEventListener("focus", change)
      window.addEventListener("blur", change)
      window.addEventListener("pagehide", change)
      return () => {
        document.removeEventListener("visibilitychange", change)
        window.removeEventListener("focus", change)
        window.removeEventListener("blur", change)
        window.removeEventListener("pagehide", change)
      }
    },
    every(ms, tick) {
      const id = window.setInterval(tick, ms)
      return () => window.clearInterval(id)
    },
  }
}

/** The local daemon's report. A console reached through Cloud does not report, so its pushes are never held. */
export function localViewingReport(bot: string, conversation: string, viewing: boolean): void {
  void fetch("/v1/project-assistants/" + encodeURIComponent(bot) + "/viewing", {
    method: "POST",
    cache: "no-store",
    keepalive: true,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ conversation, viewing }),
  }).catch(() => {})
}
