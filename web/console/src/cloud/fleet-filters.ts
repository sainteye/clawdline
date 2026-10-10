import type { ProjectedSession } from "./all-machine-sessions.js"

export type FleetFilter = "all" | "attention" | "working"

export function matchesFleetFilter(row: ProjectedSession, filter: FleetFilter): boolean {
  switch (filter) {
    case "attention": return row.waitingForReply === true || row.needsAttention === true
    case "working": return row.state === "working" && row.freshness === "current"
    default: return true
  }
}
