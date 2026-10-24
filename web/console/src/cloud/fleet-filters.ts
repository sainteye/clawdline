import type { SessionRow } from "@clawdline/contract"
import type { ProjectedSession } from "./all-machine-sessions.js"

import type { SessionStateFilter } from "../session/state-filter.js"
export type FleetFilter = SessionStateFilter

export function matchesFleetFilter(row: ProjectedSession, filter: FleetFilter, status?: Pick<Partial<SessionRow>, "shells">): boolean {
  const hasShells = (status?.shells?.length ?? 0) > 0
  switch (filter) {
    case "attention": return row.waitingForReply === true || row.needsAttention === true
    case "working": return (row.state === "working" || hasShells) && row.freshness === "current"
    case "idle": return row.state === "idle" && !hasShells && row.freshness === "current"
    default: return true
  }
}
