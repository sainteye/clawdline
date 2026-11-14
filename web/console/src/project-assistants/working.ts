import type * as W from "@clawdline/contract"

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
