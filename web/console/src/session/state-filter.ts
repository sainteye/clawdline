import type { SessionRow } from "@clawdline/contract"

export type SessionStateFilter = "all" | "attention" | "working" | "idle"
export type StateFilterCounts = Record<SessionStateFilter, number>
export const SESSION_STATE_FILTERS: readonly SessionStateFilter[] = ["all", "attention", "working", "idle"]

export function matchesCurrentSessionState(state: string, filter: "working" | "idle", current: boolean): boolean {
  return current && state === filter
}

/** Local rows expose the same user signals as the projected fleet, under wire names. */
export function matchesLocalStateFilter(row: SessionRow, filter: SessionStateFilter): boolean {
  switch (filter) {
    case "attention": return row.state === "waiting" || row.work_person_needed === true || (row.attention_count ?? 0) > 0
    case "working":
    case "idle": return matchesCurrentSessionState(row.state, filter, !row.source || row.source.freshness === "current")
    default: return true
  }
}

export function stateFilterCounts<T>(rows: readonly T[], matches: (row: T, filter: SessionStateFilter) => boolean): StateFilterCounts {
  return Object.fromEntries(SESSION_STATE_FILTERS.map((filter) => [filter, rows.filter((row) => matches(row, filter)).length])) as StateFilterCounts
}
