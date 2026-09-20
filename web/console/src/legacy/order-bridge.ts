// The list's order, fed from the copied modules' state.
//
// `derive.js` `ordered()`, `freezeOrder()` and `thawOrder()` are replaced here
// rather than edited, because that file is a byte-for-byte copy. The rule
// itself — the original's, with the most recently moved session first inside
// each state — is `session/order.ts`, and when a session last moved comes with
// the row (`activity`) rather than from a clock this browser keeps. This file
// only hands the rule the state the copied modules keep (`S`), and keeps the
// one piece of state the order has of its own: the hold a pointer puts on it.
import type { SessionRow, TaskRow } from "@clawdline/contract"
import { S } from "./js/core/state.js"
import { callSessionUI } from "./js/session/ui.js"
import { taskShaping } from "./js/view/derive.js"
import { arrangeSessions, waitingKey, type OrderHold } from "../session/order.js"

let hold: OrderHold | null = null

/** The copied state, as the order reads it. `publish` in `bridge.ts` fills it. */
function state(): { sessions: SessionRow[]; tasks: TaskRow[]; filter: string } {
  const s = S as Record<string, unknown>
  return {
    sessions: (s.sessions as SessionRow[] | undefined) ?? [],
    tasks: (s.tasks as TaskRow[] | undefined) ?? [],
    filter: typeof s.filter === "string" ? s.filter : "",
  }
}

/** The list's own order and filter, so rows are arranged as the list draws them. */
export function orderedRows(): SessionRow[] {
  const now = state()
  // Given up the moment the set of waiting sessions changes: a new question
  // outranks a steady list, and getting to the top is the point of that state.
  if (hold && hold.waiting !== waitingKey(now.sessions)) hold = null
  return arrangeSessions({
    sessions: now.sessions,
    filter: now.filter,
    tasks: now.tasks,
    shaping: taskShaping as (task: TaskRow) => boolean,
    hold,
  })
}

/** Hold the list's order while the reader's pointer is over it, as the original does. */
export function freezeOrder(): void {
  if (!hold) hold = { order: orderedRows().map((row) => row.id), waiting: waitingKey(state().sessions) }
}

/** Release it and redraw through the seam `bindSessionUI` bound. */
export function thawOrder(): void {
  if (!hold) return
  hold = null
  callSessionUI("renderList")
}
