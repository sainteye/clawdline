import type * as W from "@clawdline/contract"

export const EventRefreshMs = 3000
/** While the live roster stream reports this Bot's changes, the timer is only a fallback. */
export const PushedEventFallbackMs = 15000

/** A send awaiting its receipt keeps the ordinary cadence; it never depends on a pushed hint. */
export const chatEventDelay=(pushed:boolean,observing:boolean)=>pushed&&!observing?PushedEventFallbackMs:EventRefreshMs

/**
 * The roster frame only says when to read; the chat's own cursor read decides what
 * changed. Equal signals mean a repeated or reordered frame, which asks for nothing.
 */
export const chatPushSignal=(s:W.ProjectAssistantBotSummary)=>JSON.stringify([s.version,s.activity_at,s.unread_count,s.latest_message,s.status,s.activity,s.pending_permissions])
