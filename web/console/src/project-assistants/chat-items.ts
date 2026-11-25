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
type QueueTurn = Pick<W.ProjectAssistantReadTurnSummary,"message_id"|"state"|"queue_edit_id"|"failure_field">
const activeQueueState = (state:string) => state==="running" || state==="waiting_permission" || state==="queued" || state==="accepted"
/**
 * How many turns run before this message's own turn, in queue order: the
 * read view lists pending turns in the order the worker starts them.
 */
export function queueAhead(messageID: string, turns: readonly QueueTurn[]): number {
  const index = turns.findIndex(t=>t.message_id===messageID && (t.state==="queued"||t.state==="accepted"))
  return index < 0 ? 0 : turns.slice(0,index).filter(t=>activeQueueState(t.state)).length
}
/** The waiting label of a queued message, with how much runs before it. */
export function queuedLabel(messageID:string, turns:readonly QueueTurn[], paused:boolean, w:{queued:string;queuePaused:string;queuedAhead:string}):string {
  const ahead=queueAhead(messageID,turns)
  return (paused?w.queuePaused:w.queued)+(ahead>0?" · "+w.queuedAhead.replace("{count}",String(ahead)):"")
}
export function partitionMessages(messages: readonly W.ProjectAssistantMessage[], turns: readonly QueueTurn[]) {
  const waiting = turns.some(t=>t.state==="running" || t.state==="waiting_permission" || t.state==="queued" && t.queue_edit_id || t.state==="unknown" && (t.failure_field==="restart"||t.failure_field==="restart_confirmed"))
  // A message with other work ahead of it is waiting even while nothing
  // runs yet; only the next message to start belongs in the conversation.
  const queuedIDs = new Set(turns.filter(t=>(t.state==="queued" || t.state==="accepted") && (waiting || queueAhead(t.message_id,turns)>0)).map(t=>t.message_id))
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

/** The work-review notice a message carries: its task and notice key. */
export function workReviewNotice(message:W.ProjectAssistantMessage):{task:string;key:string}|undefined {
  if(message.kind!=="notice")return
  const notice=message.notice
  if(notice?.cause&&["work_review","work_review_waiting","work_review_held","restart_continuation"].includes(notice.key))
    return {task:notice.cause,key:notice.key}
  const held=/^work-completion-(?:refused|limit)-(.+)$/.exec(message.id)
  if(held)return {task:held[1],key:"work_review_held"}
  const review=/^This is a report on work you dispatched, not a new instruction from the user\. Task ([a-z0-9-]+), "/.exec(message.text)
  if(review)return {task:review[1],key:"work_review"}
}
/**
 * What the running turn is answering when it is not the person's message:
 * a dispatched task's report, or the continuation after a restart.
 */
export function workingSubject(turn:Pick<W.ProjectAssistantReadTurnSummary,"message_id">|undefined, timeline:readonly W.ProjectAssistantReadViewItem[], w:{workingOnReport:string;workingOnReportUntitled:string;workingOnRestart:string}):string {
  const message=turn&&timeline.find(row=>row.message?.id===turn.message_id)?.message
  const works=timeline.flatMap(row=>row.work?[row.work]:[])
  const notice=message&&workReviewNotice(message)
  if(!notice||notice.key==="work_review_held")return ""
  if(notice.key==="restart_continuation")return w.workingOnRestart
  const title=works.find(work=>work.task_id===notice.task)?.title?.trim()
  return title?w.workingOnReport.replace("{title}",title):w.workingOnReportUntitled
}

type OrderedTurn = Pick<W.ProjectAssistantReadTurnSummary,"id"|"state"|"started_at"|"queue_edit_id"|"origin"|"released_restart">
const heldAutomatic = (t:OrderedTurn) => (t.state==="queued"||t.state==="accepted") && !t.started_at && !t.queue_edit_id &&
  (t.origin==="host_notice"||t.origin==="bot_request"||t.origin==="bot_reply")
/**
 * Places a turn from a live event in the queue order the daemon runs
 * (pa.OrderQueue): a known turn keeps its place, a new one is appended, and
 * the person's message that released a restart hold moves ahead of the
 * automatic work directly before it.
 */
export function placeTurn<T extends OrderedTurn>(turns:readonly T[], next:T):T[] {
  const at=turns.findIndex(t=>t.id===next.id)
  const out=at<0?[...turns,next]:turns.map((t,i)=>i===at?next:t)
  const i=out.indexOf(next)
  if(!next.released_restart||next.state!=="queued"&&next.state!=="accepted"||next.started_at)return out
  let j=i
  while(j>0&&heldAutomatic(out[j-1]))j--
  if(j<i){out.splice(i,1);out.splice(j,0,next)}
  return out
}
