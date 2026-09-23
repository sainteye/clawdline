import type { SessionRow } from "@clawdline/contract"
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
