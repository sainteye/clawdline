// Adapted from Codync 234219ce, apps/desktop/src/renderer/views/thread/chat-items.ts.
// Copyright 2026 Po Kai Lee and Codync contributors. Apache-2.0; see third_party/codync.
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {mergeTimeline} from "./read-views.ts"
// @ts-expect-error Node executes source extensions.
import {chatTimeBreak} from "./chat-time.ts"
export type ChatItem = { id: string; kind: "separator"; at: string } | { id: string; kind: "message"; message: W.ProjectAssistantMessage; groupStart: boolean } | { id:string; kind:"work"; work:W.ProjectAssistantWorkLink } | {id:string;kind:"permission";offer:W.ProjectAssistantPermissionOffer}
export function mergeMessages(pages: readonly (readonly W.ProjectAssistantMessage[])[]): W.ProjectAssistantMessage[] {
  const byID = new Map<string,W.ProjectAssistantMessage>()
  for (const message of pages.flat()) byID.set(message.id,message)
  return [...byID.values()].sort((a,b)=>a.seq-b.seq || a.id.localeCompare(b.id))
}
export function buildChat(messages: readonly W.ProjectAssistantMessage[]): ChatItem[] {
  return buildTimelineChat(messages.map(message=>({id:message.id,kind:"message",anchor_message_seq:message.seq,observed_event_seq:0,message})))
}
/** Work keeps its source admission position even when its result arrives after newer messages. */
export function buildTimelineChat(timeline:readonly W.ProjectAssistantReadViewItem[]):ChatItem[] {
  const out: ChatItem[] = []
  let lastAt:string|undefined,lastAuthor:string|null=null
  for (const row of mergeTimeline([timeline])) {
    if(row.work){out.push({id:row.work.id,kind:"work",work:row.work});lastAuthor=null;continue}
    if(row.permission){out.push({id:row.permission.id,kind:"permission",offer:row.permission});lastAuthor=null;continue}
    const message=row.message
    if(!message||message.kind==="notice"&&message.id.startsWith("lead-call-"))continue
    const conversational=message.kind==="user"||message.kind==="assistant"
    if(conversational){
      if(chatTimeBreak(message.at,lastAt)){
        out.push({id:`sep-${message.id}`,kind:"separator",at:message.at})
        lastAuthor=null
      }
      lastAt=message.at
    }
    out.push({id:message.id,kind:"message",message,groupStart:message.kind!==lastAuthor})
    lastAuthor=conversational?message.kind:null
  }
  return out
}
/** Keep the paused message and the queue it blocks in the tray even when the Bot becomes idle. */
export function partitionMessages(messages: readonly W.ProjectAssistantMessage[], turns: readonly Pick<W.ProjectAssistantReadTurnSummary,"message_id"|"state"|"queue_edit_id"|"failure_field">[]) {
  const waiting = turns.some(t=>t.state==="running" || t.state==="waiting_permission" || t.state==="queued" && t.queue_edit_id || t.state==="unknown" && (t.failure_field==="restart"||t.failure_field==="restart_confirmed"))
  const queuedIDs = new Set(waiting ? turns.filter(t=>t.state==="queued" || t.state==="accepted").map(t=>t.message_id) : [])
  return { queued: messages.filter(m=>m.kind==="user" && queuedIDs.has(m.id)), chat: messages.filter(m=>(!queuedIDs.has(m.id) || m.kind!=="user") && !(m.kind==="notice"&&m.id.startsWith("lead-call-"))) }
}

/**
 * A message being sent is shown in an optimistic sending bubble only until
 * the conversation admits it. Once the message appears in the timeline above
 * the baseline sequence number from when sending began, the sending bubble is
 * suppressed so the message is never shown twice.
 */
export function isSendingAdmitted(sending: string | null, messages: readonly W.ProjectAssistantMessage[], baselineSeq: number): boolean {
  if (!sending) return false
  return messages.some(m => m.kind === "user" && m.seq > baselineSeq && m.text === sending)
}
