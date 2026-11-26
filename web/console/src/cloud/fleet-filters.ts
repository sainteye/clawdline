import type { ProjectedSession } from "./all-machine-sessions.js"

// @ts-expect-error -- Node's type-stripping runner resolves the source .ts file.
import { matchesCurrentSessionState, type SessionStateFilter } from "../session/state-filter.ts"
export type FleetFilter = SessionStateFilter

export function matchesFleetFilter(row: ProjectedSession, filter: FleetFilter): boolean {
  switch (filter) {
    case "attention": return row.waitingForReply === true || row.needsAttention === true
    case "working":
    case "idle": return matchesCurrentSessionState(row.state, filter, row.freshness === "current")
    default: return true
  }
}
