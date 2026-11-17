import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"

export interface BotBrowserStatus {
  owner?: string
  mode?: string
  status: { tabs: { tab: number; conversation: string; browser_mode?: string; person?: boolean; current: boolean }[] }
}

/** Older sources may ignore Bot target fields and expose unrelated tabs. */
export function botBrowserSupported(source: Pick<SourceClient,"browser"|"browserUnavailable">): boolean {
  return !!source.browser && !source.browserUnavailable?.()
}

export function botOwnedBrowserTab(status: BotBrowserStatus | null): number | null {
  if (!status?.owner) return null
  const mine = status.status.tabs.filter(tab => tab.conversation === status.owner && (!tab.browser_mode || tab.browser_mode === status.mode))
  return (mine.find(tab => tab.current) ?? mine.at(-1))?.tab ?? null
}

export function toolTraceCategory(category: string): boolean {
  return category === "tool_call" || category === "tool_call_update" || category === "tool"
}

export function browserToolTrace(trace: W.ProjectAssistantTrace): boolean {
  return toolTraceCategory(trace.category) && /(?:^|[^a-zA-Z0-9])browser_(?:status|command)(?=$|[^a-zA-Z0-9_])/.test(trace.text)
}

export function botReplyHasBrowser(message: W.ProjectAssistantMessage, traces: readonly W.ProjectAssistantTrace[]): boolean {
  return message.kind === "assistant" && !!message.turn_id && traces.some(trace => trace.turn_id === message.turn_id && browserToolTrace(trace))
}
