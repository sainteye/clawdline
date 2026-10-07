import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import type { Assistant, SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"

export interface SessionWorkCounts {
  board: number
  todos: number
  unfinished: number
}

/** The terminal observation answers whether a Session is working right now. */
export function sessionActivityName(state: SessionRow["state"]): string {
  return ({ working: "Working", waiting: catalogWord("literal", "ad8aadf7bf19"), idle: "Idle", unknown: catalogWord("literal", "68c054f499d6") } as const)[state] ?? catalogWord("literal", "68c054f499d6")
}

/** Work state explains what the Session needs even when its terminal is idle. */
export function sessionWorkStateName(state: SessionRow["work_state"]): string {
  return ({
    working: catalogWord("literal", "6db78eb344c7"),
    waiting_you: catalogWord("literal", "098adc2b04e2"),
    waiting_session: catalogWord("literal", "619d09f68466"),
    holding: catalogWord("literal", "92d2ca17aee9"),
    milestone_complete: catalogWord("literal", "f3a995d001b1"),
    work_complete: catalogWord("literal", "389a958c1c0b"),
    ready: catalogWord("literal", "65901131fccc"),
    unknown: catalogWord("literal", "6815fa90cb2c"),
  } as const)[state] ?? catalogWord("literal", "6815fa90cb2c")
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
    return catalogFormat("template", "19566a87444b", [acceptance.title, (acceptance.count ?? 0) > 1 ? catalogFormat("template", "b98d29fea593", [acceptance.count]) : ""])
  }
  const said = row.work_state === "work_complete" ? sessionWorkStateName(row.work_state) : catalogWord("literal", "8b456ffd3d10")
  // Only a reading of none is none: a missing field (an older daemon) is not.
  return acceptance?.state === "none" ? said : catalogFormat("template", "3d747201508d", [said])
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
  return assistant === "claude" ? "Claude Code" : assistant === "codex" ? "Codex" : catalogWord("literal", "1d6ea8ca2af5")
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
