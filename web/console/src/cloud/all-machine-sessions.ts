import type { AssistantSkill, Icon, SessionRow, TranscriptEntry } from "@clawdline/contract"
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
  paired?: boolean
}

/** Metadata from a pinned, read_transcript-authorized list read. */
export interface SessionListPresentation {
  title: string
  cwd?: string
  icon?: Icon
  /** Display fields from the same per-machine batch read as the title. */
  status?: Partial<SessionRow>
}

export interface MachineListPresentation extends SessionListPresentation {
  destination: SessionDestination
}

/** Keep a prior list reading for the same execution, unless fresh ss/ contradicts its state. */
export function displayedPresentationStatus(row: ProjectedSession,
  presentation: SessionListPresentation | undefined): Partial<SessionRow> | undefined {
  const status = presentation?.status
  return row.freshness === "current" && status?.state && status.state !== row.state ? undefined : status
}

export interface ProjectedSession {
  destination: SessionDestination
  /**
   * There is no name here, and there never was.
   *
   * These rows come from the content-free `ss/` status projection, which
   * carries no content by design — a name is content. The field used to hold
   * the session id so the list always had *something* to draw, and what it
   * drew was `%12` where a person expected the Session they named. A name
   * arrives only with the pinned list read (`readMachinePresentations`), so
   * until it does the list waits for its authorized presentation read.
   */
  title?: undefined
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

/**
 * Why a Session's conversation is not on screen, and the one press that
 * answers it. A reason with nothing a person can do about it carries no
 * action; a reason that has one must carry it, because the sentence alone
 * used to be the whole screen.
 */
export type FleetDetailProblem = { text: string; action?: { label: string; run: () => void } }

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
  | { kind: "unavailable"; reason: "no_permission" | "old_version" | "unknown" | "offline" | "stale" | "changed" | "unconfirmed" }

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
  subscribe(listener: (event: { machineID: string; kind: "changed" | "gap" | "detail_changed" | "access_changed"; sessionID?: string }) => void): () => void
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
    !["stale", "offline", "event_gap", "unknown", "unresponsive"].includes(reply.reason) ||
    rows.some((row) => row.freshness === "current")))
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

/**
 * The same Session's current execution, when the one a person opened is gone.
 *
 * An assistant that restarts keeps its terminal id and gets a new generation
 * (`docs/cloud-wire.md`), so a pinned read that was fine a minute ago refuses.
 * Refusing the read is right; stranding the person on it is not. This names
 * the row that replaced it, so the console can re-pin to a generation the
 * machine itself published as current instead of showing a dead sentence.
 *
 * It is not a way around the pin: the answer is a complete three-part
 * destination from a fresh `ready` projection, and the caller reads it exactly
 * as it reads any other opened row. Ambiguity refuses — with no single current
 * row for that Session there is nothing to follow.
 */
export function supersedingDestination(destination: SessionDestination,
  projection: MachineSessionProjection | undefined): SessionDestination | null {
  if (!projection || projection.kind !== "ready") return null
  if (destinationAvailable(destination, projection) !== "changed") return null
  const rows = projection.rows.filter((row) =>
    row.destination.machineID === destination.machineID &&
    row.destination.sessionID === destination.sessionID &&
    row.freshness === "current")
  return rows.length === 1 ? rows[0].destination : null
}

export function afterEventGap(before: MachineSessionProjection | undefined): MachineSessionProjection {
  return {
    kind: "unavailable", reason: "event_gap",
    rows: before?.rows?.map((row) => ({ ...row, freshness: "stale" })),
    observedAt: before?.observedAt,
    snapshotGeneration: before?.snapshotGeneration,
  }
}

/** A temporary status read failure cannot erase the last visible list or authorize its actions. */
export function settleProjection(machineID: string, before: MachineSessionProjection | undefined,
  incoming: MachineSessionProjection): MachineSessionProjection {
  const checked = checkedProjection(machineID, incoming)
  if (checked.kind !== "unavailable" || checked.rows ||
    !["event_gap", "unknown", "unresponsive", "stale", "offline"].includes(checked.reason)) return checked
  if (checked.reason === "event_gap") return afterEventGap(before)
  return before?.rows?.length ? {
    kind: "unavailable", reason: checked.reason,
    rows: before.rows.map((row) => ({ ...row, freshness: "stale" })),
    observedAt: before.observedAt, snapshotGeneration: before.snapshotGeneration,
    unknownTargets: before.unknownTargets, retryAt: checked.retryAt,
  } : checked
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
