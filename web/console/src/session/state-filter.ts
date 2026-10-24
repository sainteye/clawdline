import type { SessionRow } from "@clawdline/contract"

export type SessionStateFilter = "all" | "attention" | "working" | "idle"
export type StateFilterCounts = Record<SessionStateFilter, number>
export const SESSION_STATE_FILTERS: readonly SessionStateFilter[] = ["all", "attention", "working", "idle"]

/** Local rows expose the same user signals as the projected fleet, under wire names. */
export function matchesLocalStateFilter(row: SessionRow, filter: SessionStateFilter): boolean {
  const shells = (row.shells?.length ?? 0) > 0
  switch (filter) {
    case "attention": return row.state === "waiting" || row.work_person_needed === true || (row.attention_count ?? 0) > 0
    case "working": return (!row.source || row.source.freshness === "current") && (row.state === "working" || shells)
    case "idle": return (!row.source || row.source.freshness === "current") && row.state === "idle" && !shells
    default: return true
  }
}

export function stateFilterCounts<T>(rows: readonly T[], matches: (row: T, filter: SessionStateFilter) => boolean): StateFilterCounts {
  return Object.fromEntries(SESSION_STATE_FILTERS.map((filter) => [filter, rows.filter((row) => matches(row, filter)).length])) as StateFilterCounts
}
