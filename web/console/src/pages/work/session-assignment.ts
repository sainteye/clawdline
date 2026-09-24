import type { Assistant, SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"

export interface SessionWorkCounts {
  board: number
  todos: number
  unfinished: number
}

/** The terminal observation answers whether a Session is working right now. */
export function sessionActivityName(state: SessionRow["state"]): string {
  return ({ working: "Working", waiting: "等待中", idle: "Idle", unknown: "狀況不明" } as const)[state] ?? "狀況不明"
}

/** Work state explains what the Session needs even when its terminal is idle. */
export function sessionWorkStateName(state: SessionRow["work_state"]): string {
  return ({
    working: "執行中",
    waiting_you: "等你回應",
    waiting_session: "等待其他 Session",
    holding: "暫停中",
    milestone_complete: "待驗收",
    work_complete: "工作已完成",
    ready: "可接工作",
    unknown: "工作狀況不明",
  } as const)[state] ?? "工作狀況不明"
}

export function sessionWorkCounts(page: SessionWorkV2): SessionWorkCounts {
  const board = page.assigned_items.length
  const todos = page.direct_todos.length + page.assigned_items.reduce(
    (count, item) => count + (item.steps?.filter((step) => !step.done).length ?? 0), 0,
  )
  return { board, todos, unfinished: board + todos }
}

/** The assistants a new Session can be opened with, in the order the Board offers them. */
export const NEW_SESSION_ASSISTANTS: readonly Assistant[] = ["codex", "claude"]

/** Which company's assistant a Session runs, as the person reads it. */
export function assistantName(assistant: SessionRow["assistant"]): string {
  return assistant === "claude" ? "Claude Code" : assistant === "codex" ? "Codex" : "助理不明"
}

const LAST_ASSISTANT = "clawdline.work.new-session-assistant"

/** The last assistant chosen for a new Session on this browser; Codex before any choice. */
export function rememberedAssistant(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): Assistant {
  try {
    const value = storage?.getItem(LAST_ASSISTANT)
    return value === "claude" || value === "codex" ? value : "codex"
  } catch {
    return "codex"
  }
}

export function rememberAssistant(assistant: Assistant, storage: Pick<Storage, "setItem"> | undefined = globalThis.localStorage): void {
  try {
    storage?.setItem(LAST_ASSISTANT, assistant)
  } catch {
    // A private window keeps the choice for this card only.
  }
}
