import type { SessionRow, TaskRow } from "@clawdline/contract"
import type { ProjectedSession } from "./all-machine-sessions.js"
// @ts-expect-error -- Node's type-stripping runner uses the source in its focused test.
import { arrangeSessions, visualBranches, visualDepths, waitingKey, type OrderHold } from "../session/order.ts"

export function fleetWaitingKey(rows: readonly ProjectedSession[]): string {
  return waitingKey(rows.map((row) => ({ id: row.destination.sessionID, state: row.state as SessionRow["state"] })))
}

/** Feed the original Session ordering and tree rules with content-free rows. */
export function arrangeFleetRows(rows: readonly ProjectedSession[], titles: ReadonlyMap<string, string>, hold: OrderHold | null = null): {
  row: ProjectedSession; depth: number; branchThrough: boolean; ancestorThrough: boolean
}[] {
  const byID = new Map(rows.map((row) => [row.destination.sessionID, row]))
  const sessions = rows.map((row) => ({
    id: row.destination.sessionID,
    label: titles.get(row.destination.sessionID) ?? row.title,
    state: row.state,
    machine_scope: row.machineScope === true,
    activity: row.lastMovementAt === undefined ? undefined : { known: true, at: row.lastMovementAt / 1000 },
  } as SessionRow))
  const tasks = rows.filter((row) => row.parentSessionID && byID.has(row.parentSessionID))
    .map((row) => ({ child: { terminalId: row.destination.sessionID }, root: { terminalId: row.parentSessionID } } as TaskRow))
  const arranged = arrangeSessions({ sessions, filter: "", tasks, shaping: () => true, hold })
  const depths = visualDepths(arranged, tasks, () => true)
  const { through, ancestorThrough } = visualBranches(arranged, depths)
  return arranged.map((session) => ({ row: byID.get(session.id)!, depth: depths.get(session.id) ?? 0,
    branchThrough: through.has(session.id), ancestorThrough: ancestorThrough.has(session.id) }))
}
