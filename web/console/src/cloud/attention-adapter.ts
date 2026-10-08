import type { MachineStatusSnapshot, SessionState, StatusSession } from "./attention-model.ts"

/** Structural input supplied by the all-machine ss/ list. No s/ content. */
export interface ListMachine { id: string; name: string; platform: string; freshness?: "current" | "stale" | "unknown" }
export interface ListSession {
  destination: { machineID: string; sessionID: string; executionGeneration: string }
  title: string
  state: string
  freshness: "current" | "stale" | "unknown"
  needsAttention: boolean
  observedAt: number
  lastMovementAt?: number
  completedUnconfirmed?: boolean
  snapshotGeneration?: string
  noProgressAfterMs?: number
  noMovement?: boolean
  closeBlocked?: boolean
  failedAgentCount?: number
  /** Status-only fields forwarded from ss/.source and ss/.inventory_complete. */
  sourceProvenance?: string
  inventoryComplete?: boolean
}
export type ListProjection =
  | { kind: "ready"; complete: true; observedAt: number; rows: ListSession[]; unknownTargets?: number; snapshotGeneration?: string }
  | { kind: "unavailable"; reason: "offline" | "stale" | "unknown" | "old_version" | "no_permission" | "unresponsive" | "event_gap" | "bad_projection"; observedAt?: number; rows?: ListSession[] }
export type ListReading = { phase: "loading" } | { phase: "settled"; value: ListProjection }

const knownStates = new Set<SessionState>(["working", "waiting", "blocked", "failed", "completed", "idle", "unknown"])

/** Missing status signals stay explicit gaps until ss/ and the list expose them. */
export function fromListProjection(machine: ListMachine, reading: ListReading | undefined): MachineStatusSnapshot {
  const result = reading?.phase === "settled" ? reading.value : null
  const reason = result?.kind === "unavailable" ? result.reason : null
  const rows = result?.kind === "ready" ? result.rows : null
  const sessions: StatusSession[] | null = rows?.map((row) => ({
    target: { machine_id: row.destination.machineID, session_id: row.destination.sessionID, execution_generation: row.destination.executionGeneration },
    title: row.title,
    state: knownStates.has(row.state as SessionState) ? row.state as SessionState : "unknown",
    freshness: row.freshness,
    source: { provenance: row.sourceProvenance ?? null, observedAt: row.observedAt,
      inventoryComplete: row.inventoryComplete ?? (result?.kind === "ready" ? result.complete : null),
      snapshotGeneration: row.snapshotGeneration ?? null },
    completedUnconfirmed: row.completedUnconfirmed,
    lastMovementAt: row.lastMovementAt ?? null,
    closeBlocked: row.closeBlocked,
    failedAgentCount: row.failedAgentCount ?? null,
  })) ?? null
  const policyValues = rows?.map((row) => row.noProgressAfterMs).filter((n): n is number =>
    typeof n === "number" && Number.isFinite(n) && n > 0) ?? []
  const policy = policyValues.length === rows?.length && new Set(policyValues).size === 1 ? policyValues[0] : null
  const snapshotGeneration = result?.kind === "ready" ? result.snapshotGeneration ??
    (rows?.length && rows.every((row) => row.snapshotGeneration === rows[0].snapshotGeneration) ? rows[0].snapshotGeneration : undefined) ?? null : null
  const gaps = [
    reason ?? (!result ? "snapshot_pending" : null),
    result?.kind === "ready" && !snapshotGeneration ? "snapshot_generation_unavailable" : null,
    rows?.some((row) => !row.snapshotGeneration || !row.sourceProvenance || row.inventoryComplete === false) ? "session_source_incomplete" : null,
    result?.kind === "ready" && result.unknownTargets ? "session_target_unavailable" : null,
    rows?.some((row) => row.needsAttention && !row.completedUnconfirmed && !row.closeBlocked && !row.failedAgentCount) ? "attention_kind_unavailable" : null,
    rows?.some((row) => row.state === "waiting") ? "reply_signal_unavailable" : null,
    rows?.some((row) => row.freshness !== "current") ? "session_freshness_incomplete" : null,
    rows?.some((row) => row.state === "working" && (policy === null || typeof row.lastMovementAt !== "number")) ? "no_progress_data_unavailable" : null,
    rows?.some((row) => row.failedAgentCount === undefined || row.closeBlocked === undefined) ? "blocked_failed_signal_unavailable" : null,
    result?.kind === "ready" ? "session_blocked_failed_unavailable" : null,
  ].filter(Boolean)
  return {
    machine_id: machine.id, name: machine.name, platform: machine.platform,
    access: reason === "no_permission" ? "denied" : "readable",
    freshness: reason === "offline" ? "offline" : reason === "stale" || reason === "event_gap" || machine.freshness === "stale" ? "stale" :
      machine.freshness === "unknown" ? "unknown" : result?.kind === "ready" ? "current" : "unknown",
    completeness: result?.kind === "ready" ? "complete" : "unknown",
    observedAt: result?.observedAt ?? null,
    snapshotGeneration,
    noProgressAfterMs: policy,
    sessions,
    gap: gaps.length ? gaps.join(",") : null,
  }
}
