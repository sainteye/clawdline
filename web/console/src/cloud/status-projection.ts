import type { MachineSessionProjection, ProjectedSession } from "./all-machine-sessions.js"

const INVENTORY = "__clawdline_inventory_v1__"
// The machine restates its Cloud status every 240 seconds; the existing viewer
// freshness policy gives that heartbeat 300 seconds before it is stale.
export const STATUS_FRESH_MS = 300_000
type Held = { identity: { machine: string; session: string }; payload: unknown; observedAt: number; sequence: number }
export interface StatusProjectionClient {
  statusSnapshots?: ReadonlyMap<string, unknown>
}

export function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function held(client: StatusProjectionClient, machine: string, session: string): Held | undefined {
  const value = record(client.statusSnapshots?.get(JSON.stringify([machine, session])))
  const identity = record(value?.identity)
  if (!value || !identity || identity.machine !== machine || identity.session !== session ||
    !Number.isFinite(value.observedAt) || !Number.isSafeInteger(value.sequence)) return undefined
  return value as unknown as Held
}

/** The ss/ marker, not absence from a retained map, says which rows are complete. */
export function statusProjection(client: StatusProjectionClient | null, machine: string, nowMs = Date.now()): MachineSessionProjection {
  if (!client?.statusSnapshots) return { kind: "unavailable", reason: "old_version" }
  const marker = record(held(client, machine, INVENTORY)?.payload)
  const inventory = record(marker?.inventory)
  if (!marker || !inventory) return { kind: "unavailable", reason: "unknown" }
  const ids = inventory.sessions
  if (inventory.version !== 1 || !Array.isArray(ids) || ids.some((id) => typeof id !== "string" || !id || id === INVENTORY) ||
    new Set(ids).size !== ids.length || !Number.isFinite(marker.at) ||
    typeof marker.snapshot_generation !== "string" || !/^[0-9a-f]{32}$/u.test(marker.snapshot_generation)) {
    return { kind: "unavailable", reason: "bad_projection" }
  }
  if (marker.complete !== true) return { kind: "unavailable", reason: "unknown", observedAt: Number(marker.at) * 1000 }
  // An expired but internally consistent pass remains a readable observation.
  // Its rows cannot authorize fresh content reads or changing operations.
  let stale = outsideFreshWindow(Number(marker.at) * 1000, nowMs)
  const rows: ProjectedSession[] = []
  let unknownTargets = 0
  for (const session of ids as string[]) {
    const entry = held(client, machine, session)
    if (!entry) return { kind: "unavailable", reason: "event_gap", observedAt: Number(marker.at) * 1000 }
    const data = record(entry.payload)
    const source = record(data?.source)
    if (!data || data.machine_id !== machine || data.session_id !== session || data.inventory_complete !== true ||
      !["working", "waiting", "idle", "unknown"].includes(String(data.state)) ||
      !Number.isFinite(data.projected_at) || !Number.isFinite(source?.observed_at) ||
      !["current", "unverified", "missing"].includes(String(source?.freshness))) {
      return { kind: "unavailable", reason: "bad_projection" }
    }
    if (data.snapshot_generation !== marker.snapshot_generation) {
      return { kind: "unavailable", reason: "event_gap", observedAt: Number(marker.at) * 1000 }
    }
    const rowStale = outsideFreshWindow(Number(data.projected_at) * 1000, nowMs) ||
      (source!.freshness === "current" && outsideFreshWindow(Number(source!.observed_at) * 1000, nowMs))
    stale ||= rowStale
    if (typeof data.execution_generation !== "string" || !/^[0-9a-f]{32}$/u.test(data.execution_generation)) {
      unknownTargets += 1
      continue
    }
    rows.push({
      destination: { machineID: machine, sessionID: session, executionGeneration: data.execution_generation },
      assistant: data.assistant === "claude" || data.assistant === "codex" ? data.assistant : undefined,
      backend: data.backend === "tmux" || data.backend === "iterm" || data.backend === "ps" ? data.backend : undefined,
      parentSessionID: typeof data.parent_session_id === "string" && data.parent_session_id !== session
        ? data.parent_session_id : undefined,
      machineScope: data.machine_scope === true,
      state: String(data.state),
      freshness: stale || rowStale ? "stale" : source!.freshness === "current" ? "current" :
        source!.freshness === "unverified" ? "stale" : "unknown",
      needsAttention: typeof data.attention_required === "boolean" ? data.attention_required : undefined,
      waitingForReply: data.waiting_for_reply === true ? true : undefined,
      observedAt: Number(source!.observed_at) * 1000,
      lastMovementAt: Number.isFinite(data.last_movement_at) ? Number(data.last_movement_at) * 1000 : undefined,
      completedUnconfirmed: typeof data.completed_unconfirmed === "boolean" ? data.completed_unconfirmed : undefined,
      snapshotGeneration: typeof data.snapshot_generation === "string" ? data.snapshot_generation : undefined,
      sourceProvenance: typeof source!.provenance === "string" ? source!.provenance : undefined,
      inventoryComplete: data.inventory_complete === true,
      noProgressAfterMs: Number.isFinite(data.no_progress_after_ms) ? Number(data.no_progress_after_ms) : undefined,
      noMovement: typeof data.no_movement === "boolean" ? data.no_movement : undefined,
      closeBlocked: typeof data.close_blocked === "boolean" ? data.close_blocked : undefined,
      failedAgentCount: Number.isSafeInteger(data.failed_agent_count) && Number(data.failed_agent_count) >= 0
        ? Number(data.failed_agent_count) : undefined,
    })
  }
  if (stale) return { kind: "unavailable", reason: "stale", complete: true,
    observedAt: Number(marker.at) * 1000, snapshotGeneration: marker.snapshot_generation as string,
    rows: rows.map((row) => ({ ...row, freshness: "stale" })), unknownTargets }
  return { kind: "ready", complete: true, observedAt: Number(marker.at) * 1000,
    snapshotGeneration: marker.snapshot_generation as string,
    presentationGeneration: typeof marker.presentation_generation === "string" &&
      /^[0-9a-f]{32}$/u.test(marker.presentation_generation) ? marker.presentation_generation : undefined,
    rows, unknownTargets }
}

/** A signed, fresh inventory can complete a partial direct list only when their Session sets agree. */
export function statusInventoryIDs(client: StatusProjectionClient, machine: string, nowMs = Date.now()): readonly string[] | null {
  const marker = record(held(client, machine, INVENTORY)?.payload)
  const inventory = record(marker?.inventory)
  const ids = inventory?.sessions
  if (!marker || !inventory || inventory.version !== 1 || marker.complete !== true ||
    !Number.isFinite(marker.at) || outsideFreshWindow(Number(marker.at) * 1000, nowMs) ||
    typeof marker.snapshot_generation !== "string" || !/^[0-9a-f]{32}$/u.test(marker.snapshot_generation) ||
    !Array.isArray(ids) || ids.some((id) => typeof id !== "string" || !id || id === INVENTORY) ||
    new Set(ids).size !== ids.length) return null
  return ids as string[]
}

/** One exact retained row whose absence prevents this marker from settling. */
export function statusGapTarget(client: StatusProjectionClient, machine: string): { sessionID: string; snapshotGeneration: string } | null {
  const marker = record(held(client, machine, INVENTORY)?.payload)
  const inventory = record(marker?.inventory)
  if (!marker || !inventory || marker.complete !== true || !Array.isArray(inventory.sessions) ||
    !/^[0-9a-f]{32}$/u.test(String(marker.snapshot_generation))) return null
  for (const sessionID of inventory.sessions) {
    if (typeof sessionID !== "string" || !sessionID || sessionID === INVENTORY) return null
    const row = record(held(client, machine, sessionID)?.payload)
    if (!row || row.snapshot_generation !== marker.snapshot_generation) {
      return { sessionID, snapshotGeneration: marker.snapshot_generation as string }
    }
  }
  return null
}

/** Rows from the next signed pass can arrive before its inventory marker. */
export function statusPassTransition(client: StatusProjectionClient, machine: string): boolean {
  const marker = held(client, machine, INVENTORY)
  const payload = record(marker?.payload)
  const inventory = record(payload?.inventory)
  if (!marker || !inventory || !Array.isArray(inventory.sessions) ||
    typeof payload?.snapshot_generation !== "string") return false
  return inventory.sessions.some((sessionID) => {
    if (typeof sessionID !== "string") return false
    const row = held(client, machine, sessionID)
    return !!row && row.sequence > marker.sequence &&
      record(row.payload)?.snapshot_generation !== payload.snapshot_generation
  })
}

function outsideFreshWindow(observedAt: number, nowMs: number): boolean {
  return !Number.isFinite(observedAt) || !Number.isFinite(nowMs) ||
    Math.abs(nowMs - observedAt) > STATUS_FRESH_MS
}
