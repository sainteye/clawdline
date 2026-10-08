import { channelSegment } from "../legacy/js/net/client.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { menuFingerprint } from "../session/fingerprint.ts"
import type { MachineSessionProjection, SessionContent, SessionDestination, SessionProjectionSource } from "./all-machine-sessions.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { destinationAvailable } from "./all-machine-sessions.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { record, STATUS_FRESH_MS, statusGapTarget, statusProjection } from "./status-projection.ts"
import type { CloudClientHandle } from "./copied.js"

type StatusClient = CloudClientHandle & {
  readContentCapabilities?: ReadonlyMap<string, unknown>
  detailSnapshots?: ReadonlyMap<string, unknown>
  openDetail?(destination: SessionDestination): void
  closeDetail?(destination: SessionDestination): void
  subscribe?(channels: string[]): unknown
  unsubscribe?(channels: string[]): void
  recoverStatusRow?(machineID: string, sessionID: string, snapshotGeneration: string): Promise<boolean>
  cancelStatusRecoveries?(): void
  /** The daemon must atomically compare the execution generation before reading content. */
  infoForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  transcriptForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
}

function channels(destination: SessionDestination): string[] {
  const suffix = channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
  return ["s/" + suffix, "t/" + suffix]
}

type Question = Extract<SessionContent, { kind: "ready" }>["question"]

function supportsPinnedRead(client: StatusClient, machineID: string, nowMs = Date.now()): boolean {
  const capability = record(client.readContentCapabilities?.get(machineID))
  return capability?.supported === true && Number.isFinite(capability?.at) &&
    Math.abs(nowMs - Number(capability!.at) * 1000) <= STATUS_FRESH_MS
}

/** Rich rows are visible only after an exact s/ subscription and must name the same execution. */
export function pinnedQuestion(client: Pick<StatusClient, "detailSnapshots">, destination: SessionDestination,
  nowMs = Date.now()): Question {
  const row = record(client.detailSnapshots?.get(destination.machineID + "\u0000" + destination.sessionID))
  const identity = record(row?.identity)
  const source = record(row?.source)
  const menu = record(row?.menu)
  if (!row || !identity || identity.machine !== destination.machineID || identity.session !== destination.sessionID ||
    row.machine !== destination.machineID || row.session !== destination.sessionID ||
    row.execution_generation !== destination.executionGeneration || source?.freshness !== "current" ||
    !Number.isFinite(source.observed_at) || Math.abs(nowMs - Number(source.observed_at) * 1000) > STATUS_FRESH_MS ||
    !menu || !Array.isArray(menu.options) || menu.options.length === 0 ||
    (menu.question !== undefined && typeof menu.question !== "string") ||
    (menu.steps !== undefined && (!Array.isArray(menu.steps) || menu.steps.some((step: unknown) => {
      const part = record(step)
      return !part || typeof part.done !== "boolean" || typeof part.label !== "string"
    })))) return null
  const options = menu.options.map((option: unknown) => record(option))
  if (options.some((option: Record<string, unknown> | null) => !option || !Number.isSafeInteger(option.n) ||
    Number(option.n) < 1 || Number(option.n) > 9 || typeof option.label !== "string" ||
    typeof option.can !== "boolean")) return null
  const answerable = options.filter((option: Record<string, unknown> | null) => option!.can === true)
  if (!answerable.length || new Set(options.map((option: Record<string, unknown> | null) => option!.n)).size !== options.length) return null
  return { text: typeof menu.question === "string" ? menu.question : undefined,
    fingerprint: menuFingerprint(menu as never),
    options: answerable.map((option: Record<string, unknown> | null) => ({ key: String(option!.n), label: String(option!.label) })),
    observedAt: Number(source.observed_at) * 1000 }
}

/** Reads current client per call so token renewal does not strand the fleet page. */
export function statusSource(current: () => StatusClient | null): SessionProjectionSource {
  let attemptedClient: StatusClient | null = null
  const gapAttempts = new Map<string, { generation: string; sessions: Set<string> }>()
  const readMachine = async (machineID: string, signal: AbortSignal): Promise<MachineSessionProjection> => {
    const client = current()
    if (!client) return { kind: "unavailable", reason: "offline" }
    if (client !== attemptedClient) {
      attemptedClient = client
      gapAttempts.clear()
    }
    const projection = statusProjection(client, machineID)
    if (projection.kind === "unavailable" && projection.reason === "event_gap" && client.recoverStatusRow) {
      const missing = statusGapTarget(client, machineID)
      if (missing && !signal.aborted) {
        let attempts = gapAttempts.get(machineID)
        if (!attempts || attempts.generation !== missing.snapshotGeneration) {
          attempts = { generation: missing.snapshotGeneration, sessions: new Set() }
          gapAttempts.set(machineID, attempts)
        }
        if (!attempts.sessions.has(missing.sessionID)) {
          attempts.sessions.add(missing.sessionID)
          // Keep the gap visible while the relay restates this one missing row.
          // Its authenticated event triggers the next independent read.
          void client.recoverStatusRow(machineID, missing.sessionID, missing.snapshotGeneration).catch(() => undefined)
        }
      }
    }
    if (projection.kind !== "ready" || signal.aborted) return projection
    const answer = await client.machines()
    const machine = answer.machines.find((entry) => entry.id === machineID)
    if (!machine || machine.freshness === "unknown") return { kind: "unavailable", reason: "unknown" }
    if (machine.freshness !== "current") return { kind: "unavailable", reason: "stale", observedAt: projection.observedAt }
    return projection
  }
  return {
    readMachine,
    async readQuestion(destination, signal) {
      const client = current()
      if (!client || signal.aborted) return null
      const reading = await readMachine(destination.machineID, signal)
      if (reading.kind !== "ready" || destinationAvailable(destination, reading) !== "ready" || signal.aborted) return null
      return pinnedQuestion(client, destination)
    },
    subscribe(listener) {
      const client = current()
      const stop = client?.events((event) => {
        if (event.type === "sessions" && event.identity?.machine && event.identity?.session &&
          event.identity.session !== "__clawdline_inventory_v1__") {
          listener({ machineID: event.identity.machine, sessionID: event.identity.session, kind: "detail_changed" })
          return
        }
        if (event.type !== "session_status" || !event.identity?.machine) return
        const machineID = event.identity.machine
        const projection = statusProjection(client, machineID)
        listener({ machineID, kind: projection.kind === "unavailable" && projection.reason === "event_gap" ? "gap" : "changed" })
      })
      return () => { stop?.(); client?.cancelStatusRecoveries?.() }
    },
    async readDetail(destination, signal): Promise<SessionContent> {
      const client = current()
      if (!client) return { kind: "unavailable", reason: "offline" }
      const initialReading = await readMachine(destination.machineID, signal)
      if (initialReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(initialReading.reason) }
      const initial = destinationAvailable(destination, initialReading)
      if (initial !== "ready") return { kind: "unavailable", reason: initial === "waiting" ? "unknown" : initial }
      // A signed but older machine can still publish ss/ and has no r/ reader.
      // Refuse only its content, before the relay can mistake r/ for an offline machine.
      if (!supportsPinnedRead(client, destination.machineID)) return { kind: "unavailable", reason: "old_version" }
      if (!client.infoForGeneration || !client.transcriptForGeneration || !client.subscribe || !client.unsubscribe ||
        !client.openDetail || !client.closeDetail) {
        return { kind: "unavailable", reason: "old_version" }
      }
      const pinned = channels(destination)
      client.openDetail(destination)
      client.subscribe(pinned)
      if (signal.aborted) return { kind: "unavailable", reason: "unknown" }
      try {
        const infoReply = record(await client.infoForGeneration(destination, signal))
        if (!infoReply || signal.aborted) return { kind: "unavailable", reason: "unknown" }
        const afterInfoReading = await readMachine(destination.machineID, signal)
        if (afterInfoReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(afterInfoReading.reason) }
        const afterInfo = destinationAvailable(destination, afterInfoReading)
        if (afterInfo !== "ready") return { kind: "unavailable", reason: afterInfo === "waiting" ? "unknown" : afterInfo }
        const reply = record(await client.transcriptForGeneration(destination, signal))
        if (signal.aborted) return { kind: "unavailable", reason: "unknown" }
        const afterTranscriptReading = await readMachine(destination.machineID, signal)
        if (afterTranscriptReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(afterTranscriptReading.reason) }
        const afterTranscript = destinationAvailable(destination, afterTranscriptReading)
        if (afterTranscript !== "ready") return { kind: "unavailable", reason: afterTranscript === "waiting" ? "unknown" : afterTranscript }
        if (!reply || !Array.isArray(reply.entries)) return { kind: "unavailable", reason: "unknown" }
        const entries = reply.entries.map((entry: unknown) => record(entry)).filter((entry): entry is Record<string, unknown> => !!entry)
        if (entries.length !== reply.entries.length || entries.some((entry) => typeof entry.role !== "string" || typeof entry.text !== "string")) {
          return { kind: "unavailable", reason: "unknown" }
        }
        const session = record(record(infoReply.info)?.session)
        return { kind: "ready", destination, observedAt: Date.now(),
          info: { title: typeof session?.title === "string" ? session.title : undefined,
            assistant: typeof session?.assistant === "string" ? session.assistant : undefined,
            model: typeof session?.model === "string" ? session.model : undefined },
          entries: entries.map((entry) => ({ speaker: String(entry.role), text: String(entry.text) })),
          question: pinnedQuestion(client, destination) }
      } catch (error) {
        const code = (error as { code?: unknown } | null)?.code
        return { kind: "unavailable", reason: code === "machine_offline" ? "offline"
          : code === "execution_generation_changed" ? "changed"
          : code === "read_transcript_required" || code === "forbidden" || code === "cloud_read_needs_send_prompt" ? "no_permission"
          : code === "execution_source_unknown" ? "unknown"
          : code === "old_version" ? "old_version" : "unknown" }
      }
    },
    closeDetail(destination) {
      const client = current()
      client?.closeDetail?.(destination)
      client?.unsubscribe?.(channels(destination))
    },
  }
}

function detailProblem(reason: Extract<MachineSessionProjection, { kind: "unavailable" }>["reason"]): Extract<SessionContent, { kind: "unavailable" }>["reason"] {
  return reason === "offline" || reason === "stale" || reason === "old_version" || reason === "no_permission"
    ? reason : "unknown"
}
