import { channelSegment } from "../legacy/js/net/client.js"
import type { AssistantSkill, TranscriptEntry } from "@clawdline/contract"
import type { ArtifactRef } from "../legacy/images-bridge.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { menuFingerprint } from "../session/fingerprint.ts"
import type { MachineSessionProjection, SessionContent, SessionDestination, SessionListPresentation, SessionOlderPage,
  SessionProjectionSource } from "./all-machine-sessions.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { destinationAvailable, destinationKey } from "./all-machine-sessions.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { record, STATUS_FRESH_MS, statusGapTarget, statusPassTransition, statusProjection } from "./status-projection.ts"
import type { CloudClientHandle } from "./copied.js"

type StatusClient = CloudClientHandle & {
  /** A recent Relay machine_offline ACK, cleared by a live machine envelope. */
  machineOffline?: ReadonlyMap<string, { until: number }>
  now?(): number
  readContentCapabilities?: ReadonlyMap<string, unknown>
  detailSnapshots?: ReadonlyMap<string, unknown>
  openDetail?(destination: SessionDestination): void
  closeDetail?(destination: SessionDestination): void
  subscribe?(channels: string[]): unknown
  unsubscribe?(channels: string[]): void
  recoverStatusRow?(machineID: string, sessionID: string, snapshotGeneration: string): Promise<boolean>
  readTimeoutMs?: number
  cancelStatusRecoveries?(): void
  /** The daemon must atomically compare the execution generation before reading content. */
  infoForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  infoListForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  transcriptForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  transcriptPageForGeneration?(destination: SessionDestination, before: number, signal: AbortSignal): Promise<unknown>
  skillsForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  imageForGeneration?(destination: SessionDestination, id: string, signal: AbortSignal): Promise<unknown>
}

function channels(destination: SessionDestination): string[] {
  const suffix = channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
  return ["s/" + suffix, "t/" + suffix]
}

type Question = Extract<SessionContent, { kind: "ready" }>["question"]

function transcriptPage(reply: unknown, sessionID: string, before?: number):
  { entries: TranscriptEntry[]; nextBefore?: number } | null {
  const page = record(reply)
  if (!page || !Array.isArray(page.entries) ||
    (page.id !== undefined && page.id !== sessionID) ||
    (page.nextBefore !== undefined && (!Number.isSafeInteger(page.nextBefore) || Number(page.nextBefore) < 1 ||
      (before !== undefined && Number(page.nextBefore) >= before)))) return null
  const entries = page.entries.map((entry: unknown) => record(entry))
  if (entries.some((entry) => !entry || typeof entry.role !== "string" || typeof entry.text !== "string")) return null
  // The pinned r/ reply uses the same TranscriptEntry wire as the local reader.
  // Keep its structured cards and Unix-second timestamps for the shared renderer.
  return { entries: entries as unknown as TranscriptEntry[],
    nextBefore: page.nextBefore === undefined ? undefined : Number(page.nextBefore) }
}

function supportsPinnedRead(client: StatusClient, machineID: string, nowMs = Date.now()): boolean {
  const capability = record(client.readContentCapabilities?.get(machineID))
  return capability?.supported === true && Number.isFinite(capability?.at) &&
    Math.abs(nowMs - Number(capability!.at) * 1000) <= STATUS_FRESH_MS
}

function retainedStatus(reading: MachineSessionProjection, reason: "offline" | "stale", retryAt?: number): MachineSessionProjection {
  return { kind: "unavailable", reason, observedAt: reading.observedAt,
    rows: reading.rows?.map((row) => ({ ...row, freshness: "stale" })),
    complete: reading.kind === "ready" ? true : reading.complete,
    snapshotGeneration: reading.snapshotGeneration, unknownTargets: reading.unknownTargets, retryAt }
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
  const gapAttempts = new Map<string, { generation: string; sessions: Map<string, Promise<boolean>> }>()
  const opened = new Map<string, StatusClient>()
  const imageSignals = new Map<string, AbortController>()
  const visibleArtifacts = new Map<string, Set<string>>()
  const readMachine = async (machineID: string, signal: AbortSignal): Promise<MachineSessionProjection> => {
    const client = current()
    if (!client || client.ready === false) return { kind: "unavailable", reason: "unknown" }
    if (client !== attemptedClient) {
      attemptedClient = client
      gapAttempts.clear()
    }
    const offline = client.machineOffline?.get(machineID)
    if (offline && Number.isFinite(offline.until)) {
      const now = client.now?.() ?? Date.now()
      if (now < offline.until) return retainedStatus(statusProjection(client, machineID), "offline", offline.until + 1)
      const expired = statusProjection(client, machineID)
      return expired.kind === "unavailable" && (expired.reason === "stale" || expired.reason === "old_version") ? expired
        : { kind: "unavailable", reason: "unknown", observedAt: expired.observedAt }
    }
    let projection = statusProjection(client, machineID)
    if (projection.kind === "unavailable" && projection.reason === "event_gap" &&
      !statusPassTransition(client, machineID) && client.recoverStatusRow) {
      const missing = statusGapTarget(client, machineID)
      if (missing && !signal.aborted) {
        let attempts = gapAttempts.get(machineID)
        if (!attempts || attempts.generation !== missing.snapshotGeneration) {
          attempts = { generation: missing.snapshotGeneration, sessions: new Map() }
          gapAttempts.set(machineID, attempts)
        }
        let recovery = attempts.sessions.get(missing.sessionID)
        if (!recovery) {
          recovery = client.recoverStatusRow(machineID, missing.sessionID, missing.snapshotGeneration)
            .catch(() => false)
          attempts.sessions.set(missing.sessionID, recovery)
        }
        // A marker can overtake one row while the relay restates its cache.
        // Keep the previous list still until recovery settles, then show a
        // genuine gap if the row remains absent. Detail reads still recheck ss/.
        await recovery
        projection = statusProjection(client, machineID)
      }
    }
    if (projection.kind !== "ready" || signal.aborted) return projection
    const answer = await client.machines()
    const machine = answer.machines.find((entry) => entry.id === machineID)
    if (!machine || machine.freshness === "unknown") return { kind: "unavailable", reason: "unknown" }
    if (machine.freshness !== "current") return retainedStatus(projection, "stale")
    return projection
  }
  return {
    readMachine,
    async readListPresentation(destination, signal): Promise<SessionListPresentation | null> {
      const client = current()
      if (!client?.infoListForGeneration || !supportsPinnedRead(client, destination.machineID) || signal.aborted) return null
      const before = await readMachine(destination.machineID, signal)
      if (before.kind !== "ready" || destinationAvailable(destination, before) !== "ready") return null
      try {
        const reply = record(await client.infoListForGeneration(destination, signal))
        const after = await readMachine(destination.machineID, signal)
        if (signal.aborted || current() !== client || after.kind !== "ready" ||
          destinationAvailable(destination, after) !== "ready" || !supportsPinnedRead(client, destination.machineID)) return null
        const session = record(record(reply?.info)?.session)
        if (session?.id !== destination.sessionID || typeof session.title !== "string" || !session.title.trim()) return null
        const icon = record(session.icon)
        return { title: session.title.trim(),
          cwd: typeof session.cwd === "string" ? session.cwd : undefined,
          icon: typeof icon?.accent === "string" && Array.isArray(icon.cells) &&
            icon.cells.every((line: unknown) => Array.isArray(line) &&
              line.every((cell: unknown) => cell === null || typeof cell === "string"))
            ? { accent: icon.accent, cells: icon.cells as ((string | null)[])[] } : undefined }
      } catch { return null }
    },
    async readListTitle(destination, signal): Promise<string | null> {
      const presentation = await this.readListPresentation?.(destination, signal)
      return presentation?.title ?? null
    },
    async readImage(destination, artifact): Promise<{ url: string; release: () => void }> {
      const key = destinationKey(destination)
      const client = current()
      const controller = imageSignals.get(key)
      if (!client || opened.get(key) !== client || !controller || controller.signal.aborted ||
        !client.imageForGeneration || !visibleArtifacts.get(key)?.has(artifact.id) ||
        !supportsPinnedRead(client, destination.machineID)) {
        throw Object.assign(new Error("Session image is unavailable"), { code: "execution_generation_changed" })
      }
      const before = await readMachine(destination.machineID, controller.signal)
      if (before.kind !== "ready" || destinationAvailable(destination, before) !== "ready") {
        throw Object.assign(new Error("Session execution changed"), { code: "execution_generation_changed" })
      }
      const reply = record(await client.imageForGeneration(destination, artifact.id, controller.signal))
      const after = await readMachine(destination.machineID, controller.signal)
      if (controller.signal.aborted || opened.get(key) !== client || after.kind !== "ready" ||
        destinationAvailable(destination, after) !== "ready" || !supportsPinnedRead(client, destination.machineID)) {
        throw Object.assign(new Error("Session execution changed"), { code: "execution_generation_changed" })
      }
      if (!reply || reply.id !== artifact.id || (reply.media_type !== "image/png" && reply.media_type !== "image/jpeg") ||
        !Number.isSafeInteger(reply.byte_count) || Number(reply.byte_count) < 1 || typeof reply.data !== "string") {
        throw Object.assign(new Error("Invalid image answer"), { code: "malformed_reply" })
      }
      const raw = atob(reply.data)
      if (raw.length !== reply.byte_count) throw Object.assign(new Error("Invalid image bytes"), { code: "malformed_reply" })
      const bytes = new Uint8Array(raw.length)
      for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
      const url = URL.createObjectURL(new Blob([bytes], { type: reply.media_type }))
      return { url, release: () => URL.revokeObjectURL(url) }
    },
    async readSkills(destination, signal): Promise<AssistantSkill[]> {
      const client = current()
      if (!client || opened.get(destinationKey(destination)) !== client || !client.skillsForGeneration || signal.aborted ||
        !supportsPinnedRead(client, destination.machineID)) throw Object.assign(new Error("Session skills are unavailable"), { code: "old_version" })
      const before = await readMachine(destination.machineID, signal)
      if (before.kind !== "ready" || destinationAvailable(destination, before) !== "ready") {
        throw Object.assign(new Error("Session execution changed"), { code: "execution_generation_changed" })
      }
      const reply = record(await client.skillsForGeneration(destination, signal))
      const after = await readMachine(destination.machineID, signal)
      if (signal.aborted || opened.get(destinationKey(destination)) !== client || after.kind !== "ready" ||
        destinationAvailable(destination, after) !== "ready" || !supportsPinnedRead(client, destination.machineID)) {
        throw Object.assign(new Error("Session execution changed"), { code: "execution_generation_changed" })
      }
      if (!Array.isArray(reply?.skills) || reply.skills.some((item: unknown) => {
        const skill = record(item)
        return !skill || typeof skill.name !== "string" || typeof skill.description !== "string" || typeof skill.source !== "string"
      })) throw Object.assign(new Error("Invalid Session skills"), { code: "malformed_reply" })
      return reply.skills as AssistantSkill[]
    },
    async readQuestion(destination, signal) {
      const client = current()
      if (!client || signal.aborted) return null
      const reading = await readMachine(destination.machineID, signal)
      if (reading.kind !== "ready" || destinationAvailable(destination, reading) !== "ready" || signal.aborted) return null
      return pinnedQuestion(client, destination)
    },
    subscribe(listener) {
      const client = current()
      const transitions = new Map<string, ReturnType<typeof setTimeout>>()
      const clearTransition = (machineID: string) => {
        const timer = transitions.get(machineID)
        if (timer) clearTimeout(timer)
        transitions.delete(machineID)
      }
      const stop = client?.events((event) => {
        if ((event.type === "machine_reachability" || event.type === "orchestrator") &&
          typeof event.machine === "string") {
          listener({ machineID: event.machine, kind: "changed" })
          return
        }
        if (event.type === "sessions" && event.identity?.machine && event.identity?.session &&
          event.identity.session !== "__clawdline_inventory_v1__") {
          listener({ machineID: event.identity.machine, sessionID: event.identity.session, kind: "detail_changed" })
          return
        }
        if (event.type !== "session_status" || !event.identity?.machine) return
        const machineID = event.identity.machine
        const projection = statusProjection(client, machineID)
        if (projection.kind === "unavailable" && projection.reason === "event_gap" &&
          statusPassTransition(client, machineID)) {
          if (!transitions.has(machineID)) transitions.set(machineID, setTimeout(() => {
            transitions.delete(machineID)
            listener({ machineID, kind: "gap" })
          }, client?.readTimeoutMs ?? STATUS_FRESH_MS))
          return
        }
        clearTransition(machineID)
        listener({ machineID, sessionID: event.identity.session === "__clawdline_inventory_v1__"
          ? undefined : event.identity.session,
        kind: projection.kind === "unavailable" && projection.reason === "event_gap" ? "gap" : "changed" })
      })
      return () => { stop?.(); for (const machineID of transitions.keys()) clearTransition(machineID)
        client?.cancelStatusRecoveries?.() }
    },
    async readDetail(destination, signal): Promise<SessionContent> {
      const client = current()
      if (!client || client.ready === false) return { kind: "unavailable", reason: "unknown" }
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
      const key = destinationKey(destination)
      // Rich-row changes reread the already opened destination. Keep its exact
      // subscription in place so a retained s/ replay cannot trigger a loop.
      if (opened.get(key) !== client) {
        client.openDetail(destination)
        client.subscribe(pinned)
        opened.set(key, client)
        imageSignals.get(key)?.abort()
        imageSignals.set(key, new AbortController())
        visibleArtifacts.delete(key)
      }
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
        const page = transcriptPage(reply, destination.sessionID)
        if (!page) return { kind: "unavailable", reason: "unknown" }
        visibleArtifacts.set(key, new Set(page.entries.flatMap((entry) => entry.artifacts?.map((artifact) => artifact.id) ?? [])))
        const session = record(record(infoReply.info)?.session)
        return { kind: "ready", destination, observedAt: Date.now(),
          info: { title: typeof session?.title === "string" ? session.title : undefined,
            assistant: typeof session?.assistant === "string" ? session.assistant : undefined,
            model: typeof session?.model === "string" ? session.model : undefined },
          entries: page.entries, nextBefore: page.nextBefore,
          question: pinnedQuestion(client, destination) }
      } catch (error) {
        return { kind: "unavailable", reason: contentFailure(error) }
      }
    },
    async readOlder(destination, before, signal): Promise<SessionOlderPage> {
      const client = current()
      if (!client || opened.get(destinationKey(destination)) !== client || signal.aborted ||
        !Number.isSafeInteger(before) || before < 1) return { kind: "unavailable", reason: "unknown" }
      const initial = await readMachine(destination.machineID, signal)
      if (initial.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(initial.reason) }
      const availability = destinationAvailable(destination, initial)
      if (availability !== "ready") return { kind: "unavailable", reason: availability === "waiting" ? "unknown" : availability }
      if (!supportsPinnedRead(client, destination.machineID) || !client.transcriptPageForGeneration) {
        return { kind: "unavailable", reason: "old_version" }
      }
      try {
        const reply = await client.transcriptPageForGeneration(destination, before, signal)
        if (signal.aborted || opened.get(destinationKey(destination)) !== client) {
          return { kind: "unavailable", reason: "unknown" }
        }
        const after = await readMachine(destination.machineID, signal)
        if (after.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(after.reason) }
        const still = destinationAvailable(destination, after)
        if (still !== "ready") return { kind: "unavailable", reason: still === "waiting" ? "unknown" : still }
        if (!supportsPinnedRead(client, destination.machineID)) return { kind: "unavailable", reason: "old_version" }
        const page = transcriptPage(reply, destination.sessionID, before)
        if (page) for (const entry of page.entries) for (const artifact of entry.artifacts ?? []) {
          visibleArtifacts.get(destinationKey(destination))?.add(artifact.id)
        }
        return page ? { kind: "ready", destination, before, ...page } : { kind: "unavailable", reason: "unknown" }
      } catch (error) {
        return { kind: "unavailable", reason: olderFailure(error) }
      }
    },
    closeDetail(destination) {
      const key = destinationKey(destination)
      const client = opened.get(key) || current()
      opened.delete(key)
      imageSignals.get(key)?.abort()
      imageSignals.delete(key)
      visibleArtifacts.delete(key)
      client?.closeDetail?.(destination)
      client?.unsubscribe?.(channels(destination))
    },
  }
}

function contentFailure(error: unknown): Extract<SessionContent, { kind: "unavailable" }>["reason"] {
  const code = (error as { code?: unknown } | null)?.code
  return code === "machine_offline" ? "offline"
    : code === "execution_generation_changed" ? "changed"
    : code === "read_transcript_required" || code === "forbidden" || code === "cloud_read_needs_send_prompt" ? "no_permission"
    : code === "old_version" || code === "bad_request" ? "old_version" : "unknown"
}

function olderFailure(error: unknown): Extract<SessionContent, { kind: "unavailable" }>["reason"] {
  const code = (error as { code?: unknown } | null)?.code
  return code === "malformed_read" || code === "unsupported" || code === "unsupported_read"
    ? "old_version" : contentFailure(error)
}

function detailProblem(reason: Extract<MachineSessionProjection, { kind: "unavailable" }>["reason"]): Extract<SessionContent, { kind: "unavailable" }>["reason"] {
  return reason === "offline" || reason === "stale" || reason === "old_version" || reason === "no_permission"
    ? reason : "unknown"
}
