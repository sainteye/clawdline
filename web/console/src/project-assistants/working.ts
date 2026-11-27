import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botFailureText } from "./source-errors.ts"
// @ts-expect-error Node executes source extensions.
import { sessionReportLine } from "./session-report.ts"
// @ts-expect-error Node executes source extensions.
import { compactedNoticeText, legacyMemoryProposalTitle } from "./notice-text.ts"
import type { Words } from "./words"

export type BotMood = "idle" | "working" | "needsInput" | "waitingOnWork"
export function liveActivity(source:W.ProjectAssistantSourceStatus):boolean {
  return source.fresh && source.state === "online" && source.readable && source.access !== "hidden"
}
/** Captions are not needed to decide a mood. */
const silentWords:Record<string,string>=new Proxy({},{get:()=>""})
export function rosterMood(item:W.ProjectAssistantBotSummary,showDispatched=false):BotMood {
  if(!liveActivity(item.source))return "idle"
  if(showDispatched){
    const state=rosterShownState(item,true)
    return state==="needsInput"||state==="working"||state==="waitingOnWork"?state:"idle"
  }
  const state=item.status||(item.pending_permissions>0||item.activity==="waiting_permission"?"needsInput":["running","accepted","queued"].includes(item.activity)?"working":"idle")
  return state==="needsInput"?"needsInput":state==="working"?"working":"idle"
}
/** The detail head owns the active turn; historical running rows cannot animate a finished chat.
 * A background thread at work is this Bot at work while its own chat is quiet. */
export function chatMood(source:W.ProjectAssistantSourceStatus,turn:W.ProjectAssistantReadTurnSummary|undefined,pending:number,
  background:readonly Pick<W.ProjectAssistantBackgroundTaskSummary,"state">[]=[]):BotMood {
  if(!liveActivity(source))return "idle"
  const own=pending>0||turn?.state==="waiting_permission"?"needsInput":turn?.state==="running"?"working":"idle"
  if(own!=="idle")return own
  return background.some(task=>task.state==="waiting_permission")?"needsInput":background.some(task=>task.state==="active")?"working":"idle"
}

/**
 * What a running Bot is doing, from the latest trace event of its turn, as
 * Codync's working indicator shows it: a tool call's title, a writing or
 * thinking state while text streams, or nothing yet.
 */
export function activityOf(trace: W.ProjectAssistantTrace | undefined): string | "writing" | "" {
  if (!trace) return ""
  if (trace.category === "tool_call") return trace.text.split("\n",1)[0].trim()
  if (trace.category === "agent_message_chunk") return "writing"
  if (trace.category === "agent_thought_chunk") return "thinking"
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

const toolActivityWords = new Map<string, keyof Words>([
  ["browser_command", "activityBrowser"],
  ["browser_status", "activityBrowser"],
  ["send_message", "activitySendMessage"],
  ["message_bot", "activitySendMessage"],
  ["message_work", "activitySendMessage"],
  ["add_work", "activityDispatch"],
  ["dispatch_work", "activityDispatch"],
  ["work_status", "activityCheckWork"],
  ["read_memory", "activityReadMemory"],
  ["search_memory", "activityReadMemory"],
  ["edit_memory", "activityEditMemory"],
  ["bash", "backgroundRunningCommand"],
  ["exec_command", "backgroundRunningCommand"],
  ["shell_command", "backgroundRunningCommand"],
  ["run_shell_command", "backgroundRunningCommand"],
  ["read", "activityReadFile"],
  ["read_file", "activityReadFile"],
  ["edit", "activityEditFile"],
  ["write", "activityEditFile"],
  ["write_file", "activityEditFile"],
  ["apply_patch", "activityEditFile"],
  ["grep", "activitySearch"],
  ["glob", "activitySearch"],
  ["rg", "activitySearch"],
  ["toolsearch", "activitySearch"],
  ["websearch", "activityWebSearch"],
  ["web_search", "activityWebSearch"],
  ["webfetch", "activityReadPage"],
  ["web_fetch", "activityReadPage"],
])

/** Live traces and daemon summaries both carry the provider's tool title. */
export function rowActivity(activity: string, w: Record<string, string>): string {
  if (activity === "writing") return w.writingReply
  if (activity === "thinking") return w.thinking
  if (/^guardian[ _]review$/i.test(activity)) return w.reviewingAction
  if (activity === "running" || activity === "waiting_permission" || activity === "queued" || activity === "accepted") return w[activity] ?? activity
  const title = activity.trim().toLowerCase()
  const name = title.replace(/^(?:mcp__clawdline__|mcp\.clawdline\.|clawdline\.|functions\.)/, "").split(/\s*[:(]/,1)[0]
  const label = toolActivityWords.get(name)
  if (label) return w[label]
  if (/mcp__|^(?:mcp\.|clawdline\.|functions\.)|^[a-z][a-z0-9]*_[a-z0-9_]+(?=$|[\s(:])/.test(title)) return w.activityTool
  return activity
}

/** Source connection and unread are independent of the Bot's working state.
 * Older daemons retain their state-word fallback. Cached activity never animates. */
export function rosterActivity(item: W.ProjectAssistantBotSummary, w: Record<string,string>) {
  const live = liveActivity(item.source)
  const state = item.status || (item.pending_permissions > 0 || item.activity === "waiting_permission" ? "needsInput"
    : ["running","accepted","queued"].includes(item.activity) ? "working" : "idle")
  const shown = live ? state : "idle"
  let caption = compactedNoticeText({kind:item.latest_message_kind,text:item.latest_message,notice:item.latest_message_notice}, w) ?? item.latest_message
  if (item.latest_message_kind === "work" && item.latest_message) caption = w.sessionReportPreview.replace("{summary}", sessionReportLine(item.latest_message))
  if (shown === "needsInput") {
    const activity=item.activity
    const question=activity.startsWith("Asks: ")?activity.slice("Asks: ".length):undefined
    caption=item.pending_decision_kind==="memory_save"||(!item.pending_decision_kind&&legacyMemoryProposalTitle(question))?w.memoryCardTitle
      : activity.startsWith("Needs approval: ")?w.approvalActivity.replace("{action}",activity.slice("Needs approval: ".length))
      : question!==undefined?w.decisionActivity.replace("{question}",question)
      : activity && !["waiting_permission","running","accepted","queued"].includes(activity)?activity:w.pending
  }
  else if (shown === "working") caption = rowActivity(item.activity,w) || w.running
  else if (shown === "error") caption = item.failure ? botFailureText(item.failure.code,w) : w.failed
  return {state:shown,caption,live}
}

/** Dispatched Sessions still running or waiting for the person: the roster's
 * clone count. Absent or garbled counts (older sources) are zero. */
export function dispatchedCount(item: W.ProjectAssistantBotSummary): number {
  const work = item.dispatched_work
  const n = (v: unknown) => typeof v === "number" && Number.isSafeInteger(v) && v > 0 ? v : 0
  return work ? n(work.running) + n(work.needs_you) : 0
}

export function hasExecutingDispatch(work: W.ProjectAssistantDispatchedWork | undefined): boolean {
  // Older sources include queued Sessions in running without reporting how many are executing.
  const executing = work?.executing
  return typeof executing === "number" && Number.isSafeInteger(executing) && executing > 0
}

/** The roster state a person sees. Precedence is needsInput > error > working
 * > waitingOnWork > idle; waitingOnWork needs dispatched work shown and a
 * source that reports it. */
export function botRosterActivity(item: W.ProjectAssistantBotSummary, w: Record<string, string>, showDispatched = true) {
  const chat = rosterActivity(item, w)
  const work = item.background_work
  const dispatched = showDispatched && chat.live && item.dispatched_work ? item.dispatched_work : undefined
  if (dispatched && chat.state !== "needsInput") {
    const asking = dispatched.needs_you > 0
    if (asking && !(work && work.needs_input > 0)) {
      return {state:"needsInput", caption:w.rosterDispatchedNeedsYou, live:true, queuedOnly:false}
    }
    // An own failure is not hidden by work running elsewhere.
    if (chat.state === "error") return {...chat, queuedOnly:false}
  }
  if (chat.live && work && chat.state !== "needsInput") {
    if (work.needs_input > 0) {
      return {state:"needsInput", caption:w.rosterBackgroundNeedsYou, live:true, queuedOnly:false}
    }
    // An own failure outranks work running in the background.
    if (chat.state !== "working" && chat.state !== "error" && (work.working > 0 || work.waiting > 0)) {
      const counts = [
        work.working > 0 ? w.rosterBackgroundWorking.replace("{count}", String(work.working)) : "",
        work.waiting > 0 ? w.rosterBackgroundWaiting.replace("{count}", String(work.waiting)) : "",
      ].filter(Boolean)
      return {state:"working", caption:counts.join(" · "), live:true, queuedOnly:work.working === 0}
    }
  }
  if (dispatched && chat.state === "idle" && dispatched.running > 0) {
    const caption = w.rosterWaitingOnWork.replace("{count}", String(dispatched.running))
    return {state:"waitingOnWork", caption, live:true, queuedOnly:false}
  }
  return {...chat, queuedOnly:false}
}

/** The state botRosterActivity shows, without composing its caption. */
export function rosterShownState(item: W.ProjectAssistantBotSummary, showDispatched = true): string {
  return botRosterActivity(item, silentWords, showDispatched).state
}
