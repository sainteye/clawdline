import type { AssistantSkill, Icon, TranscriptEntry } from "@clawdline/contract"
import type { ArtifactRef } from "../legacy/images-bridge.js"

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

export interface FleetMachine {
  id: string
  name: string
  platform: string
  freshness: "current" | "stale" | "unknown"
}

/** Metadata from a pinned, read_transcript-authorized list read. */
export interface SessionListPresentation {
  title: string
  cwd?: string
  icon?: Icon
}

export interface MachineListPresentation extends SessionListPresentation {
  destination: SessionDestination
}

export interface ProjectedSession {
  destination: SessionDestination
  title: string
  assistant?: "claude" | "codex"
  backend?: "tmux" | "iterm" | "ps"
  parentSessionID?: string
  machineScope?: boolean
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
  | { kind: "unavailable"; reason: ProjectionProblem; observedAt?: number; rows?: ProjectedSession[]; retryAt?: number;
      complete?: true; snapshotGeneration?: string; unknownTargets?: number }

/** A selected status row for optional actions outside the original composer. */
export interface DetailActionContext {
  destination: SessionDestination
  machine: FleetMachine
  row: Extract<MachineSessionProjection, { kind: "ready" }>["rows"][number]
  projection: Extract<MachineSessionProjection, { kind: "ready" }>
  content: SessionContent | null
}

export type SessionContent =
  | { kind: "ready"; destination: SessionDestination; observedAt: number; info: { title?: string; assistant?: string; model?: string };
      entries: TranscriptEntry[];
      nextBefore?: number;
      question: { text?: string; fingerprint: string; options: { key: string; label: string }[]; observedAt: number } | null }
  | { kind: "unavailable"; reason: "no_permission" | "old_version" | "unknown" | "offline" | "stale" | "changed" }

export type SessionOlderPage =
  | { kind: "ready"; destination: SessionDestination; before: number;
      entries: TranscriptEntry[]; nextBefore?: number }
  | Extract<SessionContent, { kind: "unavailable" }>

export interface SessionProjectionSource {
  /** Must settle or reject; each machine is requested independently. */
  readMachine(machineID: string, signal: AbortSignal): Promise<MachineSessionProjection>
  /** One read_transcript-authorized display snapshot per machine; each row is joined by execution. */
  readMachinePresentations?(machineID: string, signal: AbortSignal): Promise<MachineListPresentation[] | null>
  /** Content is requested only after a person opens an exact destination. */
  readDetail(destination: SessionDestination, signal: AbortSignal): Promise<SessionContent>
  /** A person asks for one older page on the same opened, pinned detail. */
  readOlder?(destination: SessionDestination, before: number, signal: AbortSignal): Promise<SessionOlderPage>
  /** Recheck the signed rich row when an opened detail receives a new menu. */
  readQuestion?(destination: SessionDestination, signal: AbortSignal): Promise<Extract<SessionContent, { kind: "ready" }>["question"]>
  /** Read slash-menu metadata only for the open, exact execution. */
  readSkills?(destination: SessionDestination, signal: AbortSignal): Promise<AssistantSkill[]>
  /** Resolve a visible transcript artifact only for the opened execution. */
  readImage?(destination: SessionDestination, artifact: ArtifactRef): Promise<{ url: string; release: () => void }>
  /** Release both the rich Session and transcript channels on detail exit. */
  closeDetail?(destination: SessionDestination): void
  /** An event gap invalidates that machine's current projection and detail cache. */
  subscribe(listener: (event: { machineID: string; kind: "changed" | "gap" | "detail_changed"; sessionID?: string }) => void): () => void
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
  if (reply.kind === "unavailable" && !reply.rows) return reply
  const rows = reply.rows
  if (!rows) return { kind: "unavailable", reason: "bad_projection" }
  if (reply.kind === "ready" && (reply.complete !== true || !Number.isFinite(reply.observedAt)))
    return { kind: "unavailable", reason: "bad_projection" }
  if (reply.kind === "unavailable" && (!Number.isFinite(reply.observedAt) ||
    !["stale", "offline", "event_gap"].includes(reply.reason) || rows.some((row) => row.freshness === "current")))
    return { kind: "unavailable", reason: "bad_projection" }
  if (rows.some((row) =>
    row.destination.machineID !== machineID || !row.destination.sessionID ||
    !/^[0-9a-f]{32}$/u.test(row.destination.executionGeneration) || !Number.isFinite(row.observedAt) ||
    !["current", "stale", "unknown"].includes(row.freshness)
  )) return { kind: "unavailable", reason: "bad_projection" }
  const keys = new Set(rows.map((row) => destinationKey(row.destination)))
  return keys.size === rows.length ? reply : { kind: "unavailable", reason: "bad_projection" }
}

/** No content request may start from an unknown, stale, or superseded source. */
export function destinationAvailable(destination: SessionDestination, projection: MachineSessionProjection | undefined): "ready" | "waiting" | "changed" | "stale" | "unknown" {
  if (!projection || projection.kind !== "ready") return "waiting"
  const row = projection.rows.find((item) => destinationKey(item.destination) === destinationKey(destination))
  return row ? row.freshness === "current" ? "ready" : row.freshness : "changed"
}

export function afterEventGap(before: MachineSessionProjection | undefined): MachineSessionProjection {
  return {
    kind: "unavailable", reason: "event_gap",
    rows: before?.rows?.map((row) => ({ ...row, freshness: "stale" })),
    observedAt: before?.observedAt,
    snapshotGeneration: before?.snapshotGeneration,
  }
}

/** A partial status pass cannot erase the last visible list or authorize its actions. */
export function settleProjection(machineID: string, before: MachineSessionProjection | undefined,
  incoming: MachineSessionProjection): MachineSessionProjection {
  const checked = checkedProjection(machineID, incoming)
  return checked.kind === "unavailable" && checked.reason === "event_gap" ? afterEventGap(before) : checked
}

/** Schedule a status-only read when the earliest trusted status time expires. */
export function projectionRefreshAt(projection: MachineSessionProjection, freshnessMs: number): number | null {
  if (projection.kind !== "ready") return projection.reason === "offline" && Number.isFinite(projection.retryAt)
    ? projection.retryAt! : null
  return Math.min(projection.observedAt, ...projection.rows.filter((row) => row.freshness === "current")
    .map((row) => row.observedAt)) + freshnessMs + 1
}

/** An older page can only extend the exact detail and cursor that requested it. */
export function prependOlderPage(current: SessionContent, page: SessionOlderPage):
  Extract<SessionContent, { kind: "ready" }> | null {
  if (current.kind !== "ready" || page.kind !== "ready" ||
    destinationKey(current.destination) !== destinationKey(page.destination) || current.nextBefore !== page.before ||
    !Number.isSafeInteger(page.before) || page.before < 1 ||
    (page.nextBefore !== undefined && (!Number.isSafeInteger(page.nextBefore) || page.nextBefore < 1 || page.nextBefore >= page.before))) {
    return null
  }
  return { ...current, entries: [...page.entries, ...current.entries], nextBefore: page.nextBefore }
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
