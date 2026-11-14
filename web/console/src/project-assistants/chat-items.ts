// Adapted from Codync 234219ce, apps/desktop/src/renderer/views/thread/chat-items.ts.
// Copyright 2026 Po Kai Lee and Codync contributors. Apache-2.0; see third_party/codync.
import type * as W from "@clawdline/contract"
export type ChatItem = { id: string; kind: "separator"; at: string } | { id: string; kind: "message"; message: W.ProjectAssistantMessage; groupStart: boolean }
export const ChatSeparatorGapMs = 3_600_000
export function mergeMessages(pages: readonly (readonly W.ProjectAssistantMessage[])[]): W.ProjectAssistantMessage[] {
  const byID = new Map<string,W.ProjectAssistantMessage>()
  for (const message of pages.flat()) byID.set(message.id,message)
  return [...byID.values()].sort((a,b)=>a.seq-b.seq || a.id.localeCompare(b.id))
}
export function buildChat(messages: readonly W.ProjectAssistantMessage[]): ChatItem[] {
  const out: ChatItem[] = []
  let lastDate: number | null = null, lastAuthor: string | null = null
  for (const message of mergeMessages([messages])) {
    const date = Date.parse(message.at)
    if (lastDate === null || date-lastDate > ChatSeparatorGapMs) { out.push({id:`sep-${message.id}`,kind:"separator",at:message.at}); lastAuthor=null }
    out.push({id:message.id,kind:"message",message,groupStart:message.kind!==lastAuthor})
    lastAuthor=message.kind === "user" || message.kind === "assistant" ? message.kind : null
    lastDate=date
  }
  return out
}
/** A queued user message stays in exactly one surface, including the transition to idle.
 *  It waits in the queue tray only behind a turn that is running, as Codync's tray does;
 *  a message about to run next is already part of the conversation. */
export function partitionMessages(messages: readonly W.ProjectAssistantMessage[], turns: readonly Pick<W.ProjectAssistantReadTurnSummary,"message_id"|"state">[]) {
  const busy = turns.some(t=>t.state==="running" || t.state==="waiting_permission")
  const queuedIDs = new Set(busy ? turns.filter(t=>t.state==="queued" || t.state==="accepted").map(t=>t.message_id) : [])
  return { queued: messages.filter(m=>m.kind==="user" && queuedIDs.has(m.id)), chat: messages.filter(m=>!queuedIDs.has(m.id) || m.kind!=="user") }
}
