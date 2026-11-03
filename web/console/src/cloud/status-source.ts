import { SESSION_INVENTORY_ID, SESSION_INVENTORY_LIMIT, channelSegment } from "../legacy/js/net/client.js"
import type { AssistantSkill, SessionRow, TranscriptEntry } from "@clawdline/contract"
import type { ArtifactRef } from "../legacy/images-bridge.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { menuFingerprint } from "../session/fingerprint.ts"
import type { MachineSessionProjection, SessionContent, SessionDestination, MachineListPresentation, SessionOlderPage,
  SessionProjectionSource } from "./all-machine-sessions.js"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { destinationAvailable, destinationKey } from "./all-machine-sessions.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source in its focused test.
import { descriptorParents, record, STATUS_FRESH_MS, statusGapTarget, statusInventoryIDs, statusPassTransition, statusProjection } from "./status-projection.ts"
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
  recoverStatusMarker?(machineID: string): Promise<boolean>
  readTimeoutMs?: number
  cancelStatusRecoveries?(): void
  viewerEvents?: { record?(event: string, data: Record<string, unknown>, key?: string): unknown }
  /** The daemon must atomically compare the execution generation before reading content. */
  infoForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  listPresentationsForMachine?(machineID: string, signal: AbortSignal): Promise<unknown>
  refreshSessionStatus?(machineID: string, sessionID: string, signal: AbortSignal): Promise<unknown>
  transcriptForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  transcriptPageForGeneration?(destination: SessionDestination, before: number, signal: AbortSignal, revision?: string): Promise<unknown>
  skillsForGeneration?(destination: SessionDestination, signal: AbortSignal): Promise<unknown>
  imageForGeneration?(destination: SessionDestination, id: string, signal: AbortSignal, size?: "thumb"): Promise<unknown>
}

function reportListFailure(client: StatusClient, machineID: string, reason: string,
  code: string, ms: number, failures: number): void {
  try {
    client.viewerEvents?.record?.("cloud.status_list.failed", { machine: machineID, reason, code, ms,
      failures, support: pinnedReadSupport(client, machineID) },
    ["cloud.status_list.failed", machineID, code].join("|"))
  } catch { /* diagnostics cannot fail a Session read */ }
}

/**
 * The list is holding its previous set because the ss/ pass has a gap. Sent
 * once per machine and pass, so the machine's log can say which row the page
 * was missing and for how long, without asking the person what they saw.
 */
function reportGap(client: StatusClient, machineID: string, projection: MachineSessionProjection): void {
  if (projection.kind !== "unavailable" || projection.reason !== "event_gap") return
  try {
    const missing = statusGapTarget(client, machineID)
    const now = client.now?.() ?? Date.now()
    client.viewerEvents?.record?.("cloud.status_list.gap", { machine: machineID,
      missing: missing?.sessionID ?? null, transition: statusPassTransition(client, machineID),
      latest: projection.latest?.length ?? 0,
      marker_age_s: Number.isFinite(projection.observedAt) ? Math.round((now - projection.observedAt!) / 1000) : null },
    ["cloud.status_list.gap", machineID, missing?.snapshotGeneration ?? ""].join("|"))
  } catch { /* diagnostics cannot fail a Session read */ }
}

function channels(destination: SessionDestination): string[] {
  const suffix = channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
  return ["s/" + suffix, "t/" + suffix]
}

type Question = Extract<SessionContent, { kind: "ready" }>["question"]

function transcriptPage(reply: unknown, sessionID: string, before?: number):
  { entries: TranscriptEntry[]; nextBefore?: number; signature?: string } | null {
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
    ...(typeof page.signature === "string" ? { signature: page.signature } : {}),
    nextBefore: page.nextBefore === undefined ? undefined : Number(page.nextBefore) }
}

/**
 * Whether this machine can be read here, as three answers rather than two.
 *
 * `orch/` says whether a machine carries `machine.read_content_v1`, and the
 * answer is only usable while it is fresh. Not yet published and published-but
 * -stale are not the same thing as published-and-false: the first two mean this
 * browser does not know yet, and a reload reaches them routinely. Collapsing
 * them into one boolean made the console tell a person that the machine's
 * Clawdline was too old to read — about a current machine, with nothing for
 * them to update, and no way to tell whose version was being complained about.
 */
type PinnedReadSupport = "yes" | "no" | "unconfirmed"

/** Retry a missing retained status row after a refused or timed-out recovery. */
export const STATUS_GAP_RETRY_MS = 5_000
/** Missing inventory markers need a bounded replay, then a slower retry than a row gap. */
export const STATUS_MARKER_RETRY_MS = 60_000

type DirectList = { projection: Extract<MachineSessionProjection, { kind: "ready" }>; reply: Record<string, unknown> }

/** A pinned machine reply is a complete, authenticated fallback while ss/ replays a missing row. */
function directList(client: StatusClient, machineID: string, reply: unknown, nowMs: number, inventoryIDs: readonly string[] | null,
  held: MachineSessionProjection): DirectList | null {
  const body = record(reply)
  if (!body || (body.complete !== true && body.complete !== false) || !Number.isFinite(body.at) ||
    Math.abs(nowMs - Number(body.at) * 1000) > STATUS_FRESH_MS ||
    !Array.isArray(body.sessions) || body.sessions.length > SESSION_INVENTORY_LIMIT) return null
  const sessions = body.sessions.map((value: unknown) => record(value))
  if (sessions.some((row: Record<string, unknown> | null) => !row || typeof row.id !== "string" || !row.id ||
    typeof row.execution_generation !== "string" || !/^[0-9a-f]{32}$/u.test(row.execution_generation) ||
    typeof row.title !== "string" || !row.title.trim() || !record(row.status) ||
    !["working", "waiting", "idle", "unknown"].includes(String(record(row.status)?.state)))) return null
  const ids = sessions.map((row: Record<string, unknown> | null) => String(row!.id))
  if (new Set(ids).size !== ids.length) return null
  const taskParents = descriptorParents(client, machineID, ids, nowMs)
  const byConversation = new Map<string, string | null>()
  for (const row of sessions) {
    const conversation = record(row!.status)?.sessionId
    if (typeof conversation !== "string" || !conversation) continue
    byConversation.set(conversation, byConversation.has(conversation) ? null : String(row!.id))
  }
  // The signed ss/ set can prove that an incomplete local scan nevertheless
  // names every Session in that set. Without that proof, show only the rows
  // the machine did name. Absence then proves nothing, including an empty list.
  const complete = body.complete === true || !!inventoryIDs && ids.length === inventoryIDs.length &&
    ids.every((id) => inventoryIDs.includes(id))
  const snapshotGeneration = "direct:" + nowMs
  // A machine from before the list named the Clawdfather leaves both flags
  // out. The same execution's signed row, held from before this gap, still
  // knows; without it the machine's Clawdfather showed its own title and icon.
  const known = new Map((held.rows ?? (held.kind === "unavailable" ? held.latest : undefined) ?? []).map((row) =>
    [row.destination.sessionID + "\u0000" + row.destination.executionGeneration, row]))
  return { reply: body, projection: { kind: "ready", complete, observedAt: nowMs,
    snapshotGeneration,
    rows: sessions.map((row: Record<string, unknown> | null) => {
      const status = record(row!.status)!
      const ownerConversation = record(status.epic_parent)?.owner_session_id
      const listedParent = typeof status.parent_session_id === "string" && ids.includes(status.parent_session_id) &&
        status.parent_session_id !== row!.id ? status.parent_session_id : null
      const parentSessionID = listedParent ?? taskParents.get(String(row!.id)) ??
        (typeof ownerConversation === "string" ? byConversation.get(ownerConversation) : null)
      const before = known.get(String(row!.id) + "\u0000" + String(row!.execution_generation))
      const menu = record(status.menu)
      const options = menu?.options
      const replyProven = status.state === "waiting" && Array.isArray(options) && options.length > 0 &&
        (status.screen_reading === "read" && !menu?.source ||
          status.screen_reading === "unavailable" && menu?.source === "transcript")
      return { destination: { machineID, sessionID: String(row!.id),
        executionGeneration: String(row!.execution_generation) }, state: String(status.state),
      assistant: row!.assistant === "claude" || row!.assistant === "codex" || row!.assistant === "antigravity" ? row!.assistant : before?.assistant,
      backend: row!.backend === "tmux" || row!.backend === "iterm" || row!.backend === "ps" || row!.backend === "owned" || row!.backend === "claude_remote" ? row!.backend : before?.backend,
      parentSessionID: parentSessionID && parentSessionID !== row!.id ? parentSessionID : before?.parentSessionID,
      machineScope: status.machine_scope === true || before?.machineScope === true,
      coordinatorBound: status.coordinator_bound === true || before?.coordinatorBound === true,
      needsAttention: status.work_person_needed === true ||
        typeof status.attention_count === "number" && status.attention_count > 0,
      providerBlock: status.provider_block === "model_capacity" ? "model_capacity" as const : undefined,
      waitingForReply: replyProven ? true as const : undefined,
      freshness: "current" as const, observedAt: nowMs, snapshotGeneration }
    }) } }
}

function pinnedReadSupport(client: StatusClient, machineID: string, nowMs = Date.now()): PinnedReadSupport {
  const capability = record(client.readContentCapabilities?.get(machineID))
  if (!capability || !Number.isFinite(capability.at) ||
    Math.abs(nowMs - Number(capability.at) * 1000) > STATUS_FRESH_MS) return "unconfirmed"
  return capability.supported === true ? "yes" : "no"
}

function supportsPinnedRead(client: StatusClient, machineID: string, nowMs = Date.now()): boolean {
  return pinnedReadSupport(client, machineID, nowMs) !== "no"
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
/**
 * `clientChanged` is told each time `current()` starts answering a different
 * client. A token renewal builds a whole new client and puts it there
 * (`cloud-boot.js`), and a subscription left listening to the one it replaced
 * hears nothing again: a Session that a schedule opened on the machine, with
 * no press on this page to read it, stayed off the list until a reload.
 */
export function statusSource(current: () => StatusClient | null,
  clientChanged?: (listener: () => void) => () => void): SessionProjectionSource {
  let attemptedClient: StatusClient | null = null
  const gapAttempts = new Map<string, { generation: string;
    sessions: Map<string, { promise: Promise<boolean>; retryAt: number }> }>()
  const markerAttempts = new Map<string, { promise: Promise<boolean>; retryAt: number }>()
  const opened = new Map<string, StatusClient>()
  const directLists = new Map<string, { client: StatusClient; until: number; value: DirectList | null; failures: number;
    promise?: Promise<DirectList | null> }>()
  const imageSignals = new Map<string, AbortController>()
  const visibleArtifacts = new Map<string, Set<string>>()
  const readMachine = async (machineID: string, signal: AbortSignal, fresh = false): Promise<MachineSessionProjection> => {
    const client = current()
    if (!client || client.ready === false) return { kind: "unavailable", reason: "unknown" }
    if (client !== attemptedClient) {
      attemptedClient = client
      gapAttempts.clear()
      markerAttempts.clear()
      directLists.clear()
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
    // A machine the roster has stopped hearing from is stale before it is
    // anything else. The recoveries below ask that machine to restate what it
    // published, which a closed laptop never does, so a gap on it stayed
    // "reading this machine again" for as long as it was off.
    const roster = await client.machines().catch(() => null)
    if (signal.aborted) return projection
    if (roster?.machines.find((entry) => entry.id === machineID)?.freshness === "stale")
      return retainedStatus(projection, "stale")
    // Recovery must ask the machine even when the retained set looks fresh:
    // a push and its old status pass can agree with each other and both be old.
    const cachedDirect = directLists.get(machineID)
    if (!fresh && cachedDirect?.value && cachedDirect.client === client &&
      cachedDirect.until > (client.now?.() ?? Date.now()) &&
      cachedDirect.value.projection.observedAt > (projection.observedAt ?? 0) &&
      roster?.machines.some((machine) => machine.id === machineID && machine.freshness === "current"))
      return cachedDirect.value.projection
    // The roster can arrive before orch/ advertises the reader. One bounded
    // authenticated attempt is safe: an older machine refuses sessions.list,
    // while waiting for the descriptor can hold a cold browser for a minute.
    if ((fresh || projection.kind === "unavailable" &&
      (projection.reason === "unknown" || projection.reason === "event_gap" || projection.reason === "stale")) &&
      client.listPresentationsForMachine && pinnedReadSupport(client, machineID) !== "no" && !signal.aborted) {
      const now = client.now?.() ?? Date.now()
      let cached = directLists.get(machineID)
      if (fresh || !cached || cached.client !== client || cached.until <= now) {
        const started = now
        const reason = projection.kind === "unavailable" ? projection.reason : "stale"
        const failures = cached?.client === client ? cached.failures : 0
        const promise = client.listPresentationsForMachine(machineID, new AbortController().signal)
          .then((reply) => {
            const value = directList(client, machineID, reply, client.now?.() ?? Date.now(),
              statusInventoryIDs(client, machineID, client.now?.() ?? Date.now()), statusProjection(client, machineID))
            if (!value) reportListFailure(client, machineID, reason, "invalid_reply",
              (client.now?.() ?? Date.now()) - started, failures + 1)
            return value
          }, (error: unknown) => {
            const code = (error as { code?: unknown })?.code
            reportListFailure(client, machineID, reason, typeof code === "string" ? code : "untyped",
              (client.now?.() ?? Date.now()) - started, failures + 1)
            return null
          })
        cached = { client, until: now + STATUS_GAP_RETRY_MS, value: null,
          failures, promise }
        directLists.set(machineID, cached)
        void promise.then((value) => { if (directLists.get(machineID) === cached) {
          cached!.value = value
          cached!.promise = undefined
          // The first carrier failure clears quickly, while continued silence
          // backs off to the successful list's normal cache interval.
          cached!.failures = value ? 0 : Math.min(5, cached!.failures + 1)
          if (!value) cached!.until = (client.now?.() ?? Date.now()) +
            Math.min(STATUS_MARKER_RETRY_MS, STATUS_GAP_RETRY_MS * 2 ** (cached!.failures - 1))
        } })
      }
      const direct = cached.promise ? await cached.promise : cached.value
      if (direct && !signal.aborted && current() === client) {
        const answer = await client.machines()
        if (answer.machines.some((machine) => machine.id === machineID && machine.freshness === "current"))
          return direct.projection
      }
      if (fresh) return { kind: "unavailable", reason: "unknown" }
      projection = statusProjection(client, machineID)
      if (projection.kind === "unavailable" && projection.reason === "stale")
        projection = { ...projection, retryAt: cached.until }
    }
    if (projection.kind === "unavailable" && projection.reason === "unknown" &&
      !client.statusSnapshots?.has(JSON.stringify([machineID, SESSION_INVENTORY_ID])) &&
      client.recoverStatusMarker && !signal.aborted) {
      const now = client.now?.() ?? Date.now()
      let attempt = markerAttempts.get(machineID)
      if (!attempt || attempt.retryAt <= now) {
        attempt = { promise: client.recoverStatusMarker(machineID).catch(() => false),
          retryAt: Number.POSITIVE_INFINITY }
        markerAttempts.set(machineID, attempt)
      }
      if (await attempt.promise) markerAttempts.delete(machineID)
      else if (attempt.retryAt === Number.POSITIVE_INFINITY)
        attempt.retryAt = (client.now?.() ?? Date.now()) + STATUS_MARKER_RETRY_MS
      projection = statusProjection(client, machineID)
      if (projection.kind === "unavailable" && projection.reason === "unknown")
        return { ...projection, retryAt: Math.min(markerAttempts.get(machineID)?.retryAt ??
          (client.now?.() ?? Date.now()) + STATUS_MARKER_RETRY_MS,
        directLists.get(machineID)?.until ?? Number.POSITIVE_INFINITY) }
    }
    if (projection.kind === "unavailable" && projection.reason === "event_gap" &&
      !statusPassTransition(client, machineID) && client.recoverStatusRow) {
      const missing = statusGapTarget(client, machineID)
      if (missing && !signal.aborted) {
        let attempts = gapAttempts.get(machineID)
        if (!attempts || attempts.generation !== missing.snapshotGeneration) {
          attempts = { generation: missing.snapshotGeneration, sessions: new Map() }
          gapAttempts.set(machineID, attempts)
        }
        const now = client.now?.() ?? Date.now()
        let recovery = attempts.sessions.get(missing.sessionID)
        if (!recovery || recovery.retryAt <= now) {
          recovery = { promise: client.recoverStatusRow(machineID, missing.sessionID, missing.snapshotGeneration)
            .catch(() => false), retryAt: Number.POSITIVE_INFINITY }
          attempts.sessions.set(missing.sessionID, recovery)
        }
        // A marker can overtake one row while the relay restates its cache.
        // Keep the previous list still until recovery settles, then show a
        // genuine gap if the row remains absent. Detail reads still recheck ss/.
        if (await recovery.promise) attempts.sessions.delete(missing.sessionID)
        else if (recovery.retryAt === Number.POSITIVE_INFINITY)
          recovery.retryAt = (client.now?.() ?? Date.now()) + STATUS_GAP_RETRY_MS
        projection = statusProjection(client, machineID)
        if (projection.kind === "unavailable" && projection.reason === "event_gap") {
          const retryAt = attempts.sessions.get(missing.sessionID)?.retryAt ??
            (client.now?.() ?? Date.now()) + STATUS_GAP_RETRY_MS
          reportGap(client, machineID, projection)
          return { ...projection, retryAt }
        }
      }
    }
    if (projection.kind !== "ready" || signal.aborted) {
      if (projection.kind === "unavailable" && projection.reason === "unknown" && !projection.retryAt) {
        const until = directLists.get(machineID)?.until
        if (until) return { ...projection, retryAt: until }
      }
      reportGap(client, machineID, projection)
      return projection
    }
    const answer = await client.machines()
    const machine = answer.machines.find((entry) => entry.id === machineID)
    if (!machine || machine.freshness === "unknown") return { kind: "unavailable", reason: "unknown" }
    if (machine.freshness !== "current") return retainedStatus(projection, "stale")
    return projection
  }
  return {
    readMachine,
    async refreshSessionStatus(destination, signal): Promise<void> {
      const client = current()
      if (!client?.refreshSessionStatus || signal.aborted) return
      await client.refreshSessionStatus(destination.machineID, destination.sessionID, signal)
    },
    async readMachinePresentations(machineID, signal): Promise<MachineListPresentation[] | null> {
      const client = current()
      if (!client?.listPresentationsForMachine || pinnedReadSupport(client, machineID) === "no" || signal.aborted) return null
      const before = await readMachine(machineID, signal)
      if (before.kind !== "ready") return null
      try {
        const direct = before.snapshotGeneration?.startsWith("direct:") ? directLists.get(machineID) : null
        const held = direct?.client === client ? direct.value : null
        // A direct status fallback may be reused for a minute. Reuse its list
        // only briefly; otherwise a fresh fleet presentation would keep the
        // old work note after the local machine list had already moved on.
        const heldAt = Number(held?.projection.snapshotGeneration?.slice("direct:".length))
        const reply = held && held.projection.snapshotGeneration === before.snapshotGeneration &&
          Number.isFinite(heldAt) && (client.now?.() ?? Date.now()) - heldAt < STATUS_GAP_RETRY_MS
          ? held.reply : record(await client.listPresentationsForMachine(machineID, signal))
        const after = await readMachine(machineID, signal)
        if (signal.aborted || current() !== client || after.kind !== "ready" ||
          pinnedReadSupport(client, machineID) === "no" || !Array.isArray(reply?.sessions)) return null
        const listObservedAt = Number(reply.at) * 1000
        const trustworthyListTime = Number.isFinite(listObservedAt) &&
          Math.abs((client.now?.() ?? Date.now()) - listObservedAt) <= STATUS_FRESH_MS
        const currentRows = new Map(after.rows.map((row) => [destinationKey(row.destination), row]))
        const visible = new Set(before.rows.map((row) => destinationKey(row.destination)))
        const result: MachineListPresentation[] = []
        for (const raw of reply.sessions) {
          const session = record(raw)
          if (!session || typeof session.id !== "string" || typeof session.execution_generation !== "string" ||
            !/^[0-9a-f]{32}$/u.test(session.execution_generation) ||
            typeof session.title !== "string" || !session.title.trim()) continue
          const destination = { machineID, sessionID: session.id, executionGeneration: session.execution_generation }
          const key = destinationKey(destination)
          if (!visible.has(key) || !currentRows.has(key)) continue
          const icon = record(session.icon)
          const status = record(session.status)
          const statusState = status?.state
          result.push({ destination, projection: currentRows.get(key), title: session.title.trim(),
            observedAt: trustworthyListTime ? listObservedAt : undefined,
            assistant: session.assistant === "claude" || session.assistant === "codex" || session.assistant === "antigravity" ? session.assistant : undefined,
            cwd: typeof session.cwd === "string" ? session.cwd : undefined,
            projectKey: typeof session.project_key === "string" ? session.project_key : undefined,
            status: status && typeof status.work_state === "string" &&
              (statusState === "working" || statusState === "waiting" || statusState === "idle" || statusState === "unknown")
              ? status as Partial<SessionRow> : undefined,
            icon: typeof icon?.accent === "string" && Array.isArray(icon.cells) &&
              icon.cells.every((line: unknown) => Array.isArray(line) &&
                line.every((cell: unknown) => cell === null || typeof cell === "string"))
              ? { accent: icon.accent, cells: icon.cells as ((string | null)[])[],
                  svg: typeof icon.svg === "string" ? icon.svg : undefined } : undefined })
        }
        return result
      } catch { return null }
    },
    async readImage(destination, artifact, size): Promise<{ url: string; release: () => void }> {
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
      const reply = record(await client.imageForGeneration(destination, artifact.id, controller.signal, size === "thumb" ? "thumb" : undefined))
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
      let client: StatusClient | null = null
      let stop: (() => void) | undefined
      const transitions = new Map<string, ReturnType<typeof setTimeout>>()
      const clearTransition = (machineID: string) => {
        const timer = transitions.get(machineID)
        if (timer) clearTimeout(timer)
        transitions.delete(machineID)
      }
      const detach = () => {
        stop?.()
        stop = undefined
        for (const machineID of transitions.keys()) clearTransition(machineID)
        client?.cancelStatusRecoveries?.()
      }
      const attach = (): boolean => {
        const next = current()
        if (next === client) return false
        detach()
        client = next
        stop = next?.events((event) => {
          if ((event.type === "machine_reachability" || event.type === "orchestrator") &&
            typeof event.machine === "string") {
            listener({ machineID: event.machine, kind: event.type === "orchestrator" ? "access_changed" : "changed" })
            return
          }
          if (event.type === "sessions" && event.identity?.machine && event.identity?.session &&
            event.identity.session !== SESSION_INVENTORY_ID) {
            listener({ machineID: event.identity.machine, sessionID: event.identity.session, kind: "detail_changed" })
            return
          }
          if (event.type !== "session_status" || !event.identity?.machine) return
          const machineID = event.identity.machine
          const projection = statusProjection(next, machineID)
          if (projection.kind === "unavailable" && projection.reason === "event_gap" &&
            statusPassTransition(next, machineID)) {
            if (!transitions.has(machineID)) transitions.set(machineID, setTimeout(() => {
              transitions.delete(machineID)
              listener({ machineID, kind: "gap" })
            }, next?.readTimeoutMs ?? STATUS_FRESH_MS))
            return
          }
          clearTransition(machineID)
          listener({ machineID, sessionID: event.identity.session === SESSION_INVENTORY_ID
            ? undefined : event.identity.session,
          kind: projection.kind === "unavailable" && projection.reason === "event_gap" ? "gap" : "changed" })
        })
        return true
      }
      attach()
      const unwatch = clientChanged?.(() => {
        if (!attach() || !client?.statusSnapshots) return
        // Whatever arrived while the two clients handed over was heard by
        // nobody here, so every machine the new client holds is read again.
        const machines = new Set<string>()
        for (const key of client.statusSnapshots.keys()) {
          try {
            const [machineID] = JSON.parse(key) as unknown[]
            if (typeof machineID === "string" && machineID) machines.add(machineID)
          } catch { /* a key this page did not write names no machine */ }
        }
        for (const machineID of machines) listener({ machineID, kind: "changed" })
      })
      return () => { unwatch?.(); detach() }
    },
    async readDetail(destination, signal): Promise<SessionContent> {
      const client = current()
      if (!client || client.ready === false) return { kind: "unavailable", reason: "unknown" }
      const initialReading = await readMachine(destination.machineID, signal)
      if (initialReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(initialReading.reason) }
      const initial = destinationAvailable(destination, initialReading)
      if (initial !== "ready") return { kind: "unavailable", reason: initial === "waiting" ? "unknown" : initial }
      // A signed but older machine can still publish ss/ and has no r/ reader.
      // Refuse only its content, before the relay can mistake r/ for an offline
      // machine — but refuse it as what it is, never calling an unread
      // capability an old machine.
      const support = pinnedReadSupport(client, destination.machineID)
      if (support === "no") return { kind: "unavailable", reason: "old_version" }
      if (!client.infoForGeneration || !client.transcriptForGeneration || !client.subscribe || !client.unsubscribe ||
        !client.openDetail || !client.closeDetail) {
        return { kind: "unavailable", reason: "old_version" }
      }
      const pinned = channels(destination)
      const key = destinationKey(destination)
      const firstOpen = opened.get(key) !== client
      const started = Date.now()
      let infoMs: number | null = null
      let transcriptMs: number | null = null
      let outcome = "unknown"
      // Rich-row changes reread the already opened destination. Keep its exact
      // subscription in place so a retained s/ replay cannot trigger a loop.
      if (firstOpen) {
        client.openDetail(destination)
        client.subscribe(pinned)
        opened.set(key, client)
        imageSignals.get(key)?.abort()
        imageSignals.set(key, new AbortController())
        visibleArtifacts.delete(key)
      }
      try {
        if (signal.aborted) return { kind: "unavailable", reason: "unknown" }
        const infoStarted = Date.now()
        const infoReply = record(await client.infoForGeneration(destination, signal))
        infoMs = Date.now() - infoStarted
        if (!infoReply || signal.aborted) return { kind: "unavailable", reason: "unknown" }
        const afterInfoReading = await readMachine(destination.machineID, signal)
        if (afterInfoReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(afterInfoReading.reason) }
        const afterInfo = destinationAvailable(destination, afterInfoReading)
        if (afterInfo !== "ready") return { kind: "unavailable", reason: afterInfo === "waiting" ? "unknown" : afterInfo }
        const transcriptStarted = Date.now()
        const reply = record(await client.transcriptForGeneration(destination, signal))
        transcriptMs = Date.now() - transcriptStarted
        if (signal.aborted) return { kind: "unavailable", reason: "unknown" }
        const afterTranscriptReading = await readMachine(destination.machineID, signal)
        if (afterTranscriptReading.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(afterTranscriptReading.reason) }
        const afterTranscript = destinationAvailable(destination, afterTranscriptReading)
        if (afterTranscript !== "ready") return { kind: "unavailable", reason: afterTranscript === "waiting" ? "unknown" : afterTranscript }
        const page = transcriptPage(reply, destination.sessionID)
        if (!page) return { kind: "unavailable", reason: "unknown" }
        visibleArtifacts.set(key, new Set(page.entries.flatMap((entry) => entry.artifacts?.map((artifact) => artifact.id) ?? [])))
        const session = record(record(infoReply.info)?.session)
        outcome = "ready"
        return { kind: "ready", destination, observedAt: Date.now(),
          info: { title: typeof session?.title === "string" ? session.title : undefined,
            assistant: typeof session?.assistant === "string" ? session.assistant : undefined,
            model: typeof session?.model === "string" ? session.model : undefined },
          ...page,
          question: pinnedQuestion(client, destination) }
      } catch (error) {
        outcome = typeof (error as { code?: unknown } | null)?.code === "string"
          ? String((error as { code: string }).code) : "failed"
        return { kind: "unavailable", reason: contentFailure(error), code: outcome }
      } finally {
        // One content-free timing row per opened detail distinguishes waiting
        // for info from waiting for the transcript on a real phone. Subsequent
        // status-driven rereads do not consume the viewer's diagnostics budget.
        if (firstOpen) {
          try { client.viewerEvents?.record?.("cloud.detail.first_read", {
            machine: destination.machineID, session: destination.sessionID,
            outcome: signal.aborted ? "aborted" : outcome,
            total_ms: Date.now() - started, info_ms: infoMs, transcript_ms: transcriptMs,
          }, "cloud.detail.first_read|" + destination.machineID + "|" + destination.sessionID) } catch { /* diagnostics cannot fail a read */ }
        }
      }
    },
    async readOlder(destination, before, signal, revision): Promise<SessionOlderPage> {
      const client = current()
      if (!client || opened.get(destinationKey(destination)) !== client || signal.aborted ||
        !Number.isSafeInteger(before) || before < 1) return { kind: "unavailable", reason: "unknown" }
      const initial = await readMachine(destination.machineID, signal)
      if (initial.kind === "unavailable") return { kind: "unavailable", reason: detailProblem(initial.reason) }
      const availability = destinationAvailable(destination, initial)
      if (availability !== "ready") return { kind: "unavailable", reason: availability === "waiting" ? "unknown" : availability }
      const support = pinnedReadSupport(client, destination.machineID)
      if (support === "no" || !client.transcriptPageForGeneration) {
        return { kind: "unavailable", reason: "old_version" }
      }
      try {
        const reply = await client.transcriptPageForGeneration(destination, before, signal, revision)
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
        return page ? { kind: "ready", destination, before, ...page,
          ...(revision ? { requestedRevision: revision } : {}) } : { kind: "unavailable", reason: "unknown" }
      } catch (error) {
        return { kind: "unavailable", reason: olderFailure(error),
          ...((error as { code?: unknown } | null)?.code === "transcript_cursor_stale" ? { code: "transcript_cursor_stale" } : {}) }
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
