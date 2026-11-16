import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botFailureText } from "./source-errors.ts"

export type BotMood = "idle" | "working" | "needsInput"
export function liveActivity(source:W.ProjectAssistantSourceStatus):boolean {
  return source.fresh && source.state === "online" && source.readable && source.access !== "hidden"
}
export function rosterMood(item:W.ProjectAssistantBotSummary):BotMood {
  if(!liveActivity(item.source))return "idle"
  const state=item.status||(item.pending_permissions>0||item.activity==="waiting_permission"?"needsInput":["running","accepted","queued"].includes(item.activity)?"working":"idle")
  return state==="needsInput"?"needsInput":state==="working"?"working":"idle"
}
/** The detail head owns the active turn; historical running rows cannot animate a finished chat. */
export function chatMood(source:W.ProjectAssistantSourceStatus,turn:W.ProjectAssistantReadTurnSummary|undefined,pending:number):BotMood {
  if(!liveActivity(source))return "idle"
  return pending>0||turn?.state==="waiting_permission"?"needsInput":turn?.state==="running"?"working":"idle"
}

/**
 * What a running Bot is doing, from the latest trace event of its turn, as
 * Codync's working indicator shows it: a tool call's title, "writing" while
 * reply text streams, or nothing yet.
 */
export function activityOf(trace: W.ProjectAssistantTrace | undefined): string | "writing" | "" {
  if (!trace) return ""
  if (trace.category === "tool_call") return trace.text.trim()
  if (trace.category === "agent_message_chunk") return "writing"
  return ""
}

/** Codync's timer: 0:42, 12:05, 1:02:33; empty without a valid start. */
export function elapsed(startedAt: string | undefined, now: number): string {
  const since = startedAt ? Date.parse(startedAt) : NaN
  if (!Number.isFinite(since)) return ""
  const s = Math.max(0, Math.floor((now - since) / 1000))
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = String(s % 60).padStart(2, "0")
  return h ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`
}

/** The roster's activity is the running turn's state word; it is shown in
 *  the person's language, and anything else as it came. */
export function rowActivity(activity: string, w: Record<string, string>): string {
  if (activity === "writing") return w.writingReply
  if (activity === "thinking") return w.thinking
  if (activity === "running" || activity === "waiting_permission" || activity === "queued" || activity === "accepted") return w[activity] ?? activity
  return activity
}

/** Source connection and unread are independent of the Bot's working state.
 * Older daemons retain their state-word fallback. Cached activity never animates. */
export function rosterActivity(item: W.ProjectAssistantBotSummary, w: Record<string,string>) {
  const live = liveActivity(item.source)
  const state = item.status || (item.pending_permissions > 0 || item.activity === "waiting_permission" ? "needsInput"
    : ["running","accepted","queued"].includes(item.activity) ? "working" : "idle")
  const shown = live ? state : "idle"
  let caption = item.latest_message
  if (shown === "needsInput") {
    const activity=item.activity
    caption=activity.startsWith("Needs approval: ")?w.approvalActivity.replace("{action}",activity.slice("Needs approval: ".length))
      : activity.startsWith("Asks: ")?w.decisionActivity.replace("{question}",activity.slice("Asks: ".length))
      : activity && !["waiting_permission","running","accepted","queued"].includes(activity)?activity:w.pending
  }
  else if (shown === "working") caption = rowActivity(item.activity,w) || w.running
  else if (shown === "error") caption = item.failure ? botFailureText(item.failure.code,w) : w.failed
  return {state:shown,caption,live}
}
