/**
 * The hosted fleet view consumes only the content-free session status projection.
 * The Cloud client adapter belongs to the projection feature. A missing adapter
 * is an unsupported source, never an empty account or a rich `s/` fallback.
 */
export interface SessionDestination {
  machineID: string
  sessionID: string
  executionGeneration: string
}

export interface ProjectedSession {
  destination: SessionDestination
  title: string
  state: string
  freshness: "current" | "stale" | "unknown"
  needsAttention?: boolean
  waitingForReply?: true
  observedAt: number
  lastMovementAt?: number
  completedUnconfirmed?: boolean
  snapshotGeneration?: string
  sourceProvenance?: string
  inventoryComplete?: boolean
  noProgressAfterMs?: number
  noMovement?: boolean
  closeBlocked?: boolean
  failedAgentCount?: number
}

export type ProjectionProblem = "offline" | "stale" | "unknown" | "old_version" | "no_permission" | "unresponsive" | "event_gap" | "bad_projection"

export type MachineSessionProjection =
  | { kind: "ready"; complete: true; observedAt: number; snapshotGeneration?: string; rows: ProjectedSession[]; unknownTargets?: number }
  | { kind: "unavailable"; reason: ProjectionProblem; observedAt?: number; rows?: ProjectedSession[] }

export type SessionContent =
  | { kind: "ready"; destination: SessionDestination; observedAt: number; info: { title?: string; assistant?: string; model?: string };
      entries: { speaker: string; text: string }[];
      question: { text?: string; fingerprint: string; options: { key: string; label: string }[]; observedAt: number } | null }
  | { kind: "unavailable"; reason: "no_permission" | "old_version" | "unknown" | "offline" | "stale" | "changed" }

export interface SessionProjectionSource {
  /** Must settle or reject; each machine is requested independently. */
  readMachine(machineID: string, signal: AbortSignal): Promise<MachineSessionProjection>
  /** Content is requested only after a person opens an exact destination. */
  readDetail(destination: SessionDestination, signal: AbortSignal): Promise<SessionContent>
  /** Recheck the signed rich row when an opened detail receives a new menu. */
  readQuestion?(destination: SessionDestination, signal: AbortSignal): Promise<Extract<SessionContent, { kind: "ready" }>["question"]>
  /** Release both the rich Session and transcript channels on detail exit. */
  closeDetail?(destination: SessionDestination): void
  /** An event gap invalidates that machine's current projection and detail cache. */
  subscribe(listener: (event: { machineID: string; kind: "changed" | "gap" | "detail_changed"; sessionID?: string }) => void): () => void
}

export interface SessionFilters {
  machine: string
  platform: string
  state: string
  freshness: "all" | "current" | "stale" | "unknown"
  attention: "all" | "needed"
}

const DESTINATION = /^#machine=([^&]+)&session=([^&]+)&generation=([^&]+)$/

export function destinationKey(value: SessionDestination): string {
  return JSON.stringify([value.machineID, value.sessionID, value.executionGeneration])
}

export function destinationFragment(value: SessionDestination): string {
  return `#machine=${encodeURIComponent(value.machineID)}&session=${encodeURIComponent(value.sessionID)}&generation=${encodeURIComponent(value.executionGeneration)}`
}

export function destinationFromFragment(hash: string): SessionDestination | null {
  const match = DESTINATION.exec(hash)
  if (!match) return null
  try {
    const machineID = decodeURIComponent(match[1])
    const sessionID = decodeURIComponent(match[2])
    const executionGeneration = decodeURIComponent(match[3])
    if (!machineID || !sessionID || !executionGeneration) return null
    return { machineID, sessionID, executionGeneration }
  } catch {
    return null
  }
}

/** Refuse malformed or cross-machine rows instead of silently routing them. */
export function checkedProjection(machineID: string, reply: MachineSessionProjection): MachineSessionProjection {
  if (reply.kind !== "ready") return reply
  if (reply.complete !== true || !Number.isFinite(reply.observedAt) || reply.rows.some((row) =>
    row.destination.machineID !== machineID || !row.destination.sessionID ||
    !/^[0-9a-f]{32}$/u.test(row.destination.executionGeneration) || !Number.isFinite(row.observedAt) ||
    !["current", "stale", "unknown"].includes(row.freshness)
  )) return { kind: "unavailable", reason: "bad_projection" }
  const keys = new Set(reply.rows.map((row) => destinationKey(row.destination)))
  return keys.size === reply.rows.length ? reply : { kind: "unavailable", reason: "bad_projection" }
}

/** No content request may start from an unknown, stale, or superseded source. */
export function destinationAvailable(destination: SessionDestination, projection: MachineSessionProjection | undefined): "ready" | "waiting" | "changed" | "stale" | "unknown" {
  if (!projection || projection.kind !== "ready") return "waiting"
  const row = projection.rows.find((item) => destinationKey(item.destination) === destinationKey(destination))
  return row ? row.freshness === "current" ? "ready" : row.freshness : "changed"
}

export function matchesSession(row: ProjectedSession, platform: string, filters: SessionFilters): boolean {
  return (!filters.platform || platform === filters.platform) && (!filters.state || row.state === filters.state) &&
    (filters.attention !== "needed" || row.needsAttention === true) &&
    (filters.freshness === "all" || row.freshness === filters.freshness)
}

export function afterEventGap(before: MachineSessionProjection | undefined): MachineSessionProjection {
  return {
    kind: "unavailable", reason: "event_gap",
    rows: before?.kind === "ready" ? before.rows : undefined,
    observedAt: before?.observedAt,
  }
}

export class SessionDetailCache {
  private readonly entries = new Map<string, Extract<SessionContent, { kind: "ready" }>>()

  get(destination: SessionDestination): Extract<SessionContent, { kind: "ready" }> | undefined {
    return this.entries.get(destinationKey(destination))
  }

  put(content: Extract<SessionContent, { kind: "ready" }>): boolean {
    if (content.entries.some((entry) => typeof entry.text !== "string") || !Number.isFinite(content.observedAt)) return false
    this.entries.set(destinationKey(content.destination), content)
    return true
  }

  invalidateMachine(machineID: string): void {
    for (const [key, value] of this.entries) if (value.destination.machineID === machineID) this.entries.delete(key)
  }
}
