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

/**
 * The work state as a Session card says it. A Session's own turn report reads
 * 本輪已回報 unless the daemon names one of its Board items that reached
 * deploying or done and the person has not opened since: then 待驗收 and that
 * item. The rule is the legacy card's (`sessionReceiptCopy` in derive.js), so
 * both lists say the same thing about the same row.
 */
export function sessionWorkLabel(row: Pick<SessionRow, "work_state" | "disposition" | "acceptance">): string {
  const finished = row.work_state === "milestone_complete" || row.work_state === "work_complete"
  if (!finished || row.disposition?.scope !== "session") return sessionWorkStateName(row.work_state)
  const acceptance = row.acceptance
  if (acceptance?.state === "pending" && acceptance.title) {
    return `待驗收 · ${acceptance.title}${(acceptance.count ?? 0) > 1 ? ` · 共 ${acceptance.count} 項` : ""}`
  }
  const said = row.work_state === "work_complete" ? sessionWorkStateName(row.work_state) : "本輪已回報"
  return acceptance?.state === "unknown" || acceptance?.state === "pending" ? `${said} · 讀不到是否有待驗收項目` : said
}

/** The phases in which an item waits for the person to look at what was delivered. */
export function awaitsAcceptance(phase: string): boolean {
  return phase === "deploying" || phase === "done"
}

export function sessionWorkCounts(page: SessionWorkV2): SessionWorkCounts {
  const board = page.assigned_items.length
  const todos = page.direct_todos.length + page.assigned_items.reduce(
    (count, item) => count + (item.steps?.filter((step) => !step.done).length ?? 0), 0,
  )
  return { board, todos, unfinished: board + todos }
}

/**
 * The Sessions an item can be handed to: those working in its Project with a
 * conversation id, less the one that already owns it — moving an item to its
 * own owner is refused by the daemon (`assignment_unchanged`).
 */
export function assignmentCandidates(
  sessions: SessionRow[],
  item: { project: { path: string }; owner_session: string | null },
): SessionRow[] {
  return sessions.filter((s) => s.cwd === item.project.path && s.sessionId && s.sessionId !== item.owner_session)
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
