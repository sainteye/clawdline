/**
 * Trusted-viewer interpretation of the encrypted, status-only ss/ projection.
 * The Cloud service does not receive this report. A caller must pass every
 * selectable machine, including machines whose snapshot was unreadable.
 * No transcript, terminal, Git or shell field is accepted here.
 */
export type Freshness = "current" | "stale" | "offline" | "unknown"
export type Access = "readable" | "denied" | "revoked"
export type Completeness = "complete" | "partial" | "unknown"
export type AttentionKind = "reply" | "blocked" | "failed" | "no_progress" | "unconfirmed" | "offline"
export type SessionState = "working" | "waiting" | "blocked" | "failed" | "completed" | "idle" | "unknown"

export interface SessionTarget {
  machine_id: string
  session_id: string
  execution_generation: string
}

export interface StatusSession {
  target: SessionTarget
  title: string
  state: SessionState
  freshness?: "current" | "stale" | "unknown"
  source?: {
    provenance: string | null
    observedAt: number | null
    inventoryComplete: boolean | null
    snapshotGeneration: string | null
  }
  /** Status-only flags supplied by the machine, never inferred from prose. */
  waitingForReply?: boolean
  closeBlocked?: boolean
  failedAgentCount?: number | null
  completedUnconfirmed?: boolean
  /** Last observed movement in Unix milliseconds; content progress is unknown. */
  lastMovementAt?: number | null
}

export interface MachineStatusSnapshot {
  machine_id: string
  name: string
  platform: string
  access: Access
  freshness: Freshness
  completeness: Completeness
  observedAt: number | null
  snapshotGeneration: string | null
  /** Source-advertised registered policy. Null means no-movement cannot be inferred. */
  noProgressAfterMs?: number | null
  /** Null means unknown, distinct from an authoritative empty array. */
  sessions: readonly StatusSession[] | null
  /** Typed read/capability gap, if known. No raw private error text. */
  gap?: string | null
}

export interface AttentionEntry {
  kind: AttentionKind
  machine: MachineStatusSnapshot
  session: StatusSession | null
  /** Current fact, retained observation, or unknown machine condition. */
  evidence: "current" | "retained" | "unknown"
}

export interface AttentionOverview {
  machines: readonly MachineStatusSnapshot[]
  entries: readonly AttentionEntry[]
  unknownMachines: number
  complete: boolean
}

export function targetKey(target: SessionTarget): string {
  return JSON.stringify([target.machine_id, target.session_id, target.execution_generation])
}

/** Pure derivation. The caller supplies a fixed clock for reproducible reports. */
export function buildAttentionOverview(machines: readonly MachineStatusSnapshot[], now: number): AttentionOverview {
  const entries: AttentionEntry[] = []
  let unknownMachines = 0
  for (const machine of machines) {
    const readable = machine.access === "readable"
    const policyKnown = machine.sessions?.every((session) => session.state !== "working" ||
      (typeof machine.noProgressAfterMs === "number" && Number.isFinite(machine.noProgressAfterMs) && machine.noProgressAfterMs > 0 &&
        typeof session.lastMovementAt === "number" && Number.isFinite(session.lastMovementAt))) ?? false
    const sourcesKnown = machine.sessions?.every((session) => !!session.source?.provenance &&
      session.source.observedAt !== null && session.source.inventoryComplete === true &&
      session.source.snapshotGeneration === machine.snapshotGeneration) ?? false
    const known = readable && machine.sessions !== null && policyKnown && sourcesKnown && machine.sessions.every((session) => !session.freshness || session.freshness === "current") && !machine.gap && machine.completeness === "complete" &&
      machine.freshness === "current" && machine.observedAt !== null && machine.observedAt <= now && !!machine.snapshotGeneration
    if (!known) unknownMachines++
    if (machine.freshness === "offline") {
      entries.push({ kind: "offline", machine, session: null, evidence: "current" })
    }
    if (!readable || machine.sessions === null) continue
    for (const session of machine.sessions) {
      if (session.target.machine_id !== machine.machine_id || !session.target.session_id || !session.target.execution_generation) continue
      if (session.source?.snapshotGeneration && machine.snapshotGeneration && session.source.snapshotGeneration !== machine.snapshotGeneration) continue
      const evidence = machine.freshness === "unknown" || session.freshness === "unknown" ? "unknown" :
        machine.freshness === "current" && (session.freshness ?? "current") === "current" ? "current" : "retained"
      if (session.waitingForReply) entries.push({ kind: "reply", machine, session, evidence })
      if (session.closeBlocked) entries.push({ kind: "blocked", machine, session, evidence })
      if (typeof session.failedAgentCount === "number" && session.failedAgentCount > 0) entries.push({ kind: "failed", machine, session, evidence })
      if (session.completedUnconfirmed) entries.push({ kind: "unconfirmed", machine, session, evidence })
      // Time does not turn a retained or offline observation into a live claim.
      if (evidence === "current" && session.state === "working" &&
        typeof machine.noProgressAfterMs === "number" && Number.isFinite(machine.noProgressAfterMs) && machine.noProgressAfterMs > 0 &&
        typeof session.lastMovementAt === "number" && Number.isFinite(session.lastMovementAt) &&
        machine.observedAt !== null && machine.observedAt <= now &&
        session.lastMovementAt <= machine.observedAt && machine.observedAt - session.lastMovementAt >= machine.noProgressAfterMs) {
        entries.push({ kind: "no_progress", machine, session, evidence: "current" })
      }
    }
  }
  return { machines, entries, unknownMachines, complete: unknownMachines === 0 }
}

export interface AttentionFilter {
  machine?: string
  platform?: string
  state?: SessionState
  freshness?: Freshness
  kind?: AttentionKind
}

export function filterAttention(entries: readonly AttentionEntry[], filter: AttentionFilter): AttentionEntry[] {
  return entries.filter(({ machine, session, kind, evidence }) =>
    (!filter.machine || machine.machine_id === filter.machine) &&
    (!filter.platform || machine.platform === filter.platform) &&
    (!filter.state || session?.state === filter.state) &&
    (!filter.freshness || (filter.freshness === "current" ? evidence === "current" && machine.freshness === "current" :
      filter.freshness === "stale" ? machine.freshness === "stale" || (machine.freshness === "current" && evidence === "retained") :
        filter.freshness === "unknown" ? evidence === "unknown" : machine.freshness === filter.freshness)) &&
    (!filter.kind || kind === filter.kind))
}
