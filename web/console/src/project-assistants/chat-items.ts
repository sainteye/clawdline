// Adapted from Codync 234219ce, apps/desktop/src/renderer/views/thread/chat-items.ts.
// Copyright 2026 Po Kai Lee and Codync contributors. Apache-2.0; see third_party/codync.
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {mergeTimeline} from "./read-views.ts"
// @ts-expect-error Node executes source extensions.
import {chatTimeBreak} from "./chat-time.ts"
export type ChatProgress = { id:string; at:string; text:string; eventSeq:number }
export type ChatItem = { kind:"progress"; progress:ChatProgress; id:string } | { id: string; kind: "separator"; at: string } | { id: string; kind: "message"; message: W.ProjectAssistantMessage; groupStart: boolean } | { id:string; kind:"work"; work:W.ProjectAssistantWorkLink } | {id:string;kind:"permission";offer:W.ProjectAssistantPermissionOffer}
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
  let lastAt:string|undefined,lastAuthor:string|null=null
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
  while(next<updates.length)appendProgress()
  return out
}
/** Keep the paused message and the queue it blocks in the tray even when the Bot becomes idle. */
export function partitionMessages(messages: readonly W.ProjectAssistantMessage[], turns: readonly Pick<W.ProjectAssistantReadTurnSummary,"message_id"|"state"|"queue_edit_id"|"failure_field">[]) {
  const waiting = turns.some(t=>t.state==="running" || t.state==="waiting_permission" || t.state==="queued" && t.queue_edit_id || t.state==="unknown" && (t.failure_field==="restart"||t.failure_field==="restart_confirmed"))
  const queuedIDs = new Set(waiting ? turns.filter(t=>t.state==="queued" || t.state==="accepted").map(t=>t.message_id) : [])
  return { queued: messages.filter(m=>m.kind==="user" && queuedIDs.has(m.id)), chat: messages.filter(m=>(!queuedIDs.has(m.id) || m.kind!=="user") && !(m.kind==="notice"&&m.id.startsWith("lead-call-"))) }
}

/**
 * Select inline progress rows for the chat surface.
 * Only the currently active turn streams narration inline; completed turns fold
 * their narration (including any post-send_message wrap-up) back into the
 * turn's single-line collapsible processing disclosure, while exact duplicates
 * of visible reply bubbles stay suppressed from both surfaces.
 */
export function selectInlineProgress(
  progressRows: readonly W.ProjectAssistantReadViewItem[],
  timeline: readonly W.ProjectAssistantReadViewItem[],
  options: {
    activeTurnID?: string
    progressThrough: number | null
    isVisible: (trace: W.ProjectAssistantTrace) => boolean
  },
): {
  inlineProgress: ChatProgress[]
  projectedSteps: Set<string>
  completedProgressTurns: Set<string>
} {
  const inlineProgress: ChatProgress[] = []
  const projectedSteps = new Set<string>()
  const completedProgressTurns = new Set<string>()
  const replyTexts = new Map<string, Set<string>>()
  for (const row of timeline) {
    const message = row.message
    if (message?.kind !== "assistant" || message.output || message.exchange) continue
    const seen = replyTexts.get(message.turn_id) ?? new Set<string>()
    seen.add(message.text.trim())
    replyTexts.set(message.turn_id, seen)
  }
  for (const row of progressRows) {
    const step = row.trace
    const at = row.occurred_at
    if (!step || !at || options.progressThrough === null || Date.parse(at) < options.progressThrough || !options.isVisible(step)) continue
    const duplicateReply = !!replyTexts.get(step.turn_id)?.has(step.text.trim())
    if (options.activeTurnID && step.turn_id === options.activeTurnID) {
      projectedSteps.add(row.id)
      if (!duplicateReply) {
        inlineProgress.push({ id: row.id, text: step.text, at, eventSeq: row.observed_event_seq })
      }
      continue
    }
    if (duplicateReply) {
      projectedSteps.add(row.id)
      continue
    }
    completedProgressTurns.add(step.turn_id)
  }
  return { inlineProgress, projectedSteps, completedProgressTurns }
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
