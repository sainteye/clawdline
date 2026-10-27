import type { Decision, WorkV2Item, WorkV2Status } from "./api.js"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { needsPerson } from "./board-card-facts.ts"

type BoardItem = Pick<WorkV2Item, "id" | "kind" | "phase" | "area" | "closed_at" | "owner_session" | "condition" | "user_action" | "decision_id">

/** Presentation groups do not change the item's phase, owner or condition. */
export function desktopBoardItems<T extends BoardItem>(items: readonly T[], decisions: readonly Pick<Decision, "work_id">[]) {
  const groups: Record<"planning" | "unassigned" | "active" | "attention" | "done", T[]> = {
    planning: [], unassigned: [], active: [], attention: [], done: [],
  }
  const questions = new Set(decisions.map((decision) => decision.work_id))
  for (const item of items) {
    if (item.phase === "cancelled") continue
    if (item.closed_at || item.phase === "done") {
      if (item.phase === "done") groups.done.push(item)
    } else if (item.kind === "plan" || item.area === "planning") groups.planning.push(item)
    else if (needsPerson(item, questions.has(item.id) ? 1 : 0)) groups.attention.push(item)
    else if (!item.owner_session && item.phase !== "assigning") groups.unassigned.push(item)
    else groups.active.push(item)
  }
  return groups
}

/** Desktop switches are independent of the phone's existing status filter. */
export function boardReadStatus(desktop: boolean, showDone: boolean, phoneStatus: WorkV2Status): WorkV2Status {
  return desktop ? showDone ? "all" : "open" : phoneStatus
}
