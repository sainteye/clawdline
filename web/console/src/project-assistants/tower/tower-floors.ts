import type * as W from "@clawdline/contract"
import type { SourceClient } from "../client"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { authorized, errorCode, provablyNotSent } from "../client.ts"

/** One machine's floor placements, as its source last answered them. */
export interface FloorDoc {
  machine: string
  version: number
  /** project_id → floor index from the top. */
  floors: ReadonlyMap<string, number>
}

/** How a placement ended. Each is shown as it is; none of them sends again. */
export type FloorOutcome =
  | "saved"       // The machine stored it.
  | "stale"       // Another device changed the floors first; they were read again.
  | "unknown"     // It was sent and its outcome is unknown; the floors were read again.
  | "unsupported" // This machine's daemon predates floor placements.
  | "refused"     // The machine refused it; nothing changed.

export class FloorError extends Error {
  constructor(readonly outcome: Exclude<FloorOutcome, "saved">, readonly code = "") { super(outcome); this.name = "FloorError" }
}

/** Validate a source's answer before it places a single Bot. */
export function floorDoc(answer: W.ProjectAssistantTowerFloorsResponse, machine: string, floors: number): FloorDoc {
  if (!answer || answer.contract_version !== "project-assistants.v1" || answer.machine_id !== machine ||
    !Number.isSafeInteger(answer.version) || answer.version < 0 || !Array.isArray(answer.floors)) throw new FloorError("refused", "pa_invalid")
  const out = new Map<string, number>()
  for (const f of answer.floors) {
    if (!f || typeof f.project_id !== "string" || !f.project_id || out.has(f.project_id) || !Number.isInteger(f.floor) || f.floor < 0 || f.floor >= floors) throw new FloorError("refused", "pa_invalid")
    out.set(f.project_id, f.floor)
  }
  return { machine, version: answer.version, floors: out }
}

/** Placements across machines, keyed as the tower groups Projects. */
export function placedFloors(docs: Iterable<FloorDoc>): Map<string, number> {
  const out = new Map<string, number>()
  for (const d of docs) for (const [project, floor] of d.floors) out.set(JSON.stringify([d.machine, project]), floor)
  return out
}

export async function readFloors(source: SourceClient, current: () => readonly SourceClient[], floors: number, signal: AbortSignal): Promise<FloorDoc> {
  if (source.canTowerFloors?.() !== true || typeof source.towerFloors !== "function") throw new FloorError("unsupported", "pa_unsupported")
  const answer = await authorized(source, current, s => source.towerFloors!(s), signal)
  return floorDoc(answer, source.machineID, floors)
}

/**
 * Place one Project from the version this page last read. A refusal before
 * sending changes nothing. After sending, a stale version or an unknown
 * outcome reads the floors again so the page shows what the machine holds.
 */
export async function placeFloor(source: SourceClient, current: () => readonly SourceClient[], doc: FloorDoc, project: string, floor: number, floors: number, signal: AbortSignal,
  id: string = crypto.randomUUID()): Promise<FloorDoc> {
  if (source.canTowerFloors?.() !== true || typeof source.saveTowerFloor !== "function") throw new FloorError("unsupported", "pa_unsupported")
  if (doc.machine !== source.machineID || !Number.isInteger(floor) || floor < 0 || floor >= floors || !project) throw new FloorError("refused", "pa_invalid")
  const request: W.ProjectAssistantTowerFloorRequest = { machine_id: doc.machine, idempotency_key: id, expected_version: doc.version, project_id: project, floor }
  let answer: W.ProjectAssistantTowerFloorsResponse
  try {
    answer = await authorized(source, current, s => source.saveTowerFloor!(request, s), signal)
  } catch (error) {
    if ((error as Error)?.name === "AbortError" && provablyNotSent(error)) throw error
    const code = errorCode(error)
    if (provablyNotSent(error)) throw new FloorError(code === "pa_unsupported" ? "unsupported" : "refused", code)
    if (code === "pa_stale_version") throw new FloorError("stale", code)
    if (code === "pa_outcome_unknown" || code === "unavailable") throw new FloorError("unknown", code)
    throw new FloorError("refused", code)
  }
  const next = floorDoc(answer, doc.machine, floors)
  if (next.floors.get(project) !== floor) throw new FloorError("unknown", "pa_invalid")
  return next
}
