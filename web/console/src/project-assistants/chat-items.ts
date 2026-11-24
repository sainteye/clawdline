// Adapted from Codync 234219ce, apps/desktop/src/renderer/views/thread/chat-items.ts.
// Copyright 2026 Po Kai Lee and Codync contributors. Apache-2.0; see third_party/codync.
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {mergeTimeline} from "./read-views.ts"
export type ChatProgress = { id:string; at:string; text:string; eventSeq:number }
export type ChatItem = { kind:"progress"; progress:ChatProgress; id:string } | { id: string; kind: "separator"; at: string } | { id: string; kind: "message"; message: W.ProjectAssistantMessage; groupStart: boolean } | { id:string; kind:"work"; work:W.ProjectAssistantWorkLink } | {id:string;kind:"permission";offer:W.ProjectAssistantPermissionOffer}
export const ChatSeparatorGapMs = 3_600_000
export function mergeMessages(pages: readonly (readonly W.ProjectAssistantMessage[])[]): W.ProjectAssistantMessage[] {
  const byID = new Map<string,W.ProjectAssistantMessage>()
  for (const message of pages.flat()) byID.set(message.id,message)
  return [...byID.values()].sort((a,b)=>a.seq-b.seq || a.id.localeCompare(b.id))
}
export function buildChat(messages: readonly W.ProjectAssistantMessage[]): ChatItem[] {
  return buildTimelineChat(messages.map(message=>({id:message.id,kind:"message",anchor_message_seq:message.seq,observed_event_seq:0,message})))
}
/** Work keeps its source admission position even when its result arrives after newer messages. */
// Wire timestamps are UTC. Preserve the sub-millisecond digits when Date.parse ties.
function compareTimes(a:string,b:string) {
  const fraction=(value:string)=>Number((/\.(\d+)Z$/.exec(value)?.[1]??"").padEnd(9,"0"))
  return Date.parse(a)-Date.parse(b)||fraction(a)-fraction(b)
}
export function buildTimelineChat(timeline:readonly W.ProjectAssistantReadViewItem[],progress:readonly ChatProgress[]=[]):ChatItem[] {
  const out: ChatItem[] = []
  let lastDate: number | null = null, lastAuthor: string | null = null
  const updates=[...progress].sort((a,b)=>compareTimes(a.at,b.at)||a.eventSeq-b.eventSeq)
  let next=0
  const appendProgress=()=>{
    const update=updates[next++]
    out.push({kind:"progress",id:update.id,progress:update})
    lastAuthor=null
  }
  for (const row of mergeTimeline([timeline])) {
    if(row.work){out.push({id:row.work.id,kind:"work",work:row.work});lastAuthor=null;continue}
    if(row.permission){out.push({id:row.permission.id,kind:"permission",offer:row.permission});lastAuthor=null;continue}
    const message=row.message
    if(!message||message.kind==="notice"&&message.id.startsWith("lead-call-"))continue
    while(next<updates.length){
      const order=compareTimes(updates[next].at,message.at)
      if(order>0||order===0&&message.kind==="user")break
      appendProgress()
    }
    const date = Date.parse(message.at)
    if (lastDate === null || date-lastDate > ChatSeparatorGapMs) { out.push({id:`sep-${message.id}`,kind:"separator",at:message.at}); lastAuthor=null }
    out.push({id:message.id,kind:"message",message,groupStart:message.kind!==lastAuthor})
    lastAuthor=message.kind === "user" || message.kind === "assistant" ? message.kind : null
    lastDate=date
  }
  while(next<updates.length)appendProgress()
  return out
}
/** Keep the paused message and the queue it blocks in the tray even when the Bot becomes idle. */
export function partitionMessages(messages: readonly W.ProjectAssistantMessage[], turns: readonly Pick<W.ProjectAssistantReadTurnSummary,"message_id"|"state"|"queue_edit_id">[]) {
  const waiting = turns.some(t=>t.state==="running" || t.state==="waiting_permission" || t.state==="queued" && t.queue_edit_id)
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
