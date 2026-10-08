/** Cross-machine writes never infer a destination from the selected console machine. */
export interface ActionDestination {
  machineID: string
  sessionID: string
  executionGeneration: string
}

export type Action = "send" | "answer" | "interrupt" | "end"
export type ActionProblem = "offline" | "stale" | "unknown" | "changed" | "revoked" |
  "content_permission" | "operation_permission" | "unsupported" | "unavailable" | "storage_unavailable" |
  "menu_unverified" | "closeability_blocked"
export type Stage = "unknown" | "accepted" | "completed" | "rejected" | "pending" | "missing" | "observed" | "acknowledged"

export interface ActionReceipt {
  request: string
  action: Action
  execution_generation: string
  machine_execution: "missing" | "pending" | "completed" | "rejected" | "unknown"
  status: number
  code: string
  relay_accepted: string
  relay_delivered: string
  viewer_observed: string
  viewer_acknowledged: string
}

export interface ActionRecord {
  viewer: string
  destination: ActionDestination
  action: Action
  request: string
  fingerprint: string
  observed: boolean
  acknowledged: boolean
  receipt: ActionReceipt | null
  problem: string | null
}

export interface ActionContext {
  destination: ActionDestination
  machine: { id: string; name: string; freshness: "current" | "stale" | "unknown" }
  row: { destination: ActionDestination; freshness: "current" | "stale" | "unknown"; observedAt?: number; closeBlocked?: boolean }
  content: { kind: string; reason?: string; question?: {
    text?: string; fingerprint: string; options: readonly { key: string; label: string }[]; observedAt: number
  } | null } | null
}

export interface ActionProjection {
  kind: "ready" | "unavailable"
  rows?: readonly { destination: ActionDestination; freshness: "current" | "stale" | "unknown";
    observedAt?: number; closeBlocked?: boolean }[]
  reason?: string
}

export interface ActionSource {
  readMachine(machineID: string, signal: AbortSignal): Promise<ActionProjection>
}

export interface PinnedClient {
  deviceID: string | null
  allowWrites?: boolean
  viewerVerified?: ReadonlyMap<string, unknown>
  machines(): Promise<{ machines: readonly { id: string; freshness: "current" | "stale" | "unknown"; pairing: string }[] }>
  machineDescriptor?(machine: string): { machine?: { commands?: string[] } } | null
  _read?(identity: { machine: string; session: string }, type: string, body: Record<string, unknown>,
    answer: string, timeoutMs?: number, options?: { retireUncertain?: boolean }): Promise<unknown>
}

export interface ActionStore {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export interface ActionInput {
  text?: string
  answer?: string
  expect?: string
}

const generation = /^[0-9a-f]{32}$/u
const fingerprint = /^[0-9a-f]{64}$/u

export function sameDestination(a: ActionDestination, b: ActionDestination): boolean {
  return a.machineID === b.machineID && a.sessionID === b.sessionID && a.executionGeneration === b.executionGeneration
}

export function receiptPath(record: ActionRecord): string {
  return `/v1/sessions/${encodeURIComponent(record.destination.sessionID)}/cloud-receipts/${encodeURIComponent(record.request)}` +
    `?action=${encodeURIComponent(record.action)}&execution_generation=${encodeURIComponent(record.destination.executionGeneration)}`
}

export function receiptStages(record: ActionRecord): readonly Stage[] {
  const receipt = record.receipt
  return [
    receipt?.relay_accepted === "accepted" ? "accepted" : "unknown",
    receipt?.machine_execution ?? "unknown",
    receipt?.relay_delivered === "completed" ? "completed" : "unknown",
    record.observed || receipt?.viewer_observed === "observed" ? "observed" : "unknown",
    record.acknowledged ? "acknowledged" : "unknown",
  ]
}

export function problemCode(error: unknown): string {
  const code = (error as { code?: unknown } | null)?.code
  return typeof code === "string" && code ? code : "receipt_outcome_unknown"
}

/** One browser device owns its own durable lookup pointer; no message body is stored. */
export class PinnedSessionActions {
  private readonly source: ActionSource
  private readonly current: () => PinnedClient | null
  private readonly store: ActionStore
  private readonly requestID: () => string

  constructor(source: ActionSource, current: () => PinnedClient | null, store: ActionStore, requestID: () => string) {
    this.source = source
    this.current = current
    this.store = store
    this.requestID = requestID
  }

  private key(viewer: string, destination: ActionDestination, action: Action): string {
    return "clawdline.cloud.session-receipt:" + JSON.stringify([viewer, destination.machineID, destination.sessionID,
      destination.executionGeneration, action])
  }

  load(context: ActionContext, action: Action): ActionRecord | null {
    const viewer = this.current()?.deviceID
    if (!viewer) return null
    try {
      const raw = this.store.getItem(this.key(viewer, context.destination, action))
      if (!raw) return null
      const record: ActionRecord = JSON.parse(raw)
      return record.viewer === viewer && record.action === action &&
        sameDestination(record.destination, context.destination) && typeof record.request === "string" && !!record.request &&
        typeof record.fingerprint === "string"
        ? { ...record, receipt: null, observed: false, problem: "receipt_check_required" } : null
    } catch { return null }
  }

  private save(record: ActionRecord): void {
    this.store.setItem(this.key(record.viewer, record.destination, record.action), JSON.stringify(record))
  }

  /** Fresh status and capability evidence is read again immediately before every write. */
  async availability(context: ActionContext, action: Action): Promise<ActionProblem | null> {
    const target = context.destination
    if (!target.machineID || !target.sessionID || !generation.test(target.executionGeneration) ||
      context.machine.id !== target.machineID || !sameDestination(context.row.destination, target)) return "unknown"
    if (context.machine.freshness === "stale") return "stale"
    if (context.machine.freshness !== "current") return "unknown"
    const client = this.current()
    if (!client) return "offline"
    if (!client.deviceID || !client.viewerVerified?.has(target.machineID)) return "revoked"
    if (!client.allowWrites) return "operation_permission"
    if (!client._read) return "unsupported"
    const machines = await client.machines().catch(() => null)
    const machine = machines?.machines.find((entry) => entry.id === target.machineID)
    if (!machine) return "unknown"
    if (machine.pairing !== "paired") return "revoked"
    if (machine.freshness === "stale") return "stale"
    if (machine.freshness === "unknown") return "unknown"
    const commands = client.machineDescriptor?.(target.machineID)?.machine?.commands
    if (!Array.isArray(commands) || !commands.includes(action) || !commands.includes("session-receipt")) return "unsupported"
    if (action === "answer") {
      if (context.content?.kind === "unavailable") {
        const reason = context.content.reason
        return reason === "no_permission" ? "content_permission" : reason === "old_version" ? "unsupported" : "unknown"
      }
      if (context.content?.kind !== "ready") return "unknown"
      if (!context.content.question || !fingerprint.test(context.content.question.fingerprint) ||
        !context.content.question.options.some((option) => option.key && option.label)) return "menu_unverified"
    }
    const projection = await this.source.readMachine(target.machineID, new AbortController().signal).catch(() => null)
    if (!projection || projection.kind === "unavailable") {
      if (projection?.reason === "offline") return "offline"
      if (projection?.reason === "stale") return "stale"
      return "unknown"
    }
    const currentRow = projection.rows?.find((row) => row.destination.machineID === target.machineID &&
      row.destination.sessionID === target.sessionID)
    if (!currentRow) return "changed"
    if (!sameDestination(currentRow.destination, target)) return "changed"
    if (currentRow.freshness === "stale") return "stale"
    if (currentRow.freshness !== "current") return "unknown"
    if (action === "end" && currentRow.closeBlocked === true) return "closeability_blocked"
    if (action === "answer" && typeof currentRow.observedAt === "number" &&
      context.content?.question && currentRow.observedAt > context.content.question.observedAt) return "menu_unverified"
    return null
  }

  async perform(context: ActionContext, action: Action, input: ActionInput): Promise<ActionRecord> {
    const bytes = new TextEncoder().encode(JSON.stringify([action, input.text?.trim() ?? "", input.answer ?? "", input.expect ?? ""]))
    const digest = await crypto.subtle.digest("SHA-256", bytes)
    const payloadFingerprint = Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("")
    const existing = this.load(context, action)
    if (existing && !existing.acknowledged) {
      if (existing.fingerprint !== payloadFingerprint) throw Object.assign(new Error("idempotency_key_reused"), { code: "idempotency_key_reused" })
      return this.lookup(existing)
    }
    const problem = await this.availability(context, action)
    if (problem) throw Object.assign(new Error(problem), { code: problem })
    if (action === "send" && !input.text?.trim()) throw Object.assign(new Error("empty"), { code: "empty" })
    const question = context.content?.question
    if (action === "answer" && (!question || !input.answer || !input.expect || !fingerprint.test(input.expect) ||
      input.expect !== question.fingerprint || !question.options.some((option) => option.key === input.answer))) {
      throw Object.assign(new Error("menu_unverified"), { code: "menu_unverified" })
    }
    const client = this.current()!
    const viewer = client.deviceID!
    const record: ActionRecord = { viewer, destination: { ...context.destination }, action,
      request: this.requestID(), fingerprint: payloadFingerprint, observed: false, acknowledged: false, receipt: null, problem: null }
    if (!record.request) throw Object.assign(new Error("idempotency_key_required"), { code: "idempotency_key_required" })
    try { this.save(record) } catch { throw Object.assign(new Error("storage_unavailable"), { code: "storage_unavailable" }) }
    const identity = { machine: record.destination.machineID, session: record.destination.sessionID }
    const body: Record<string, unknown> = { request: record.request, execution_generation: record.destination.executionGeneration }
    if (action === "send") Object.assign(body, { text: input.text!.trim(), images: [] })
    if (action === "answer") Object.assign(body, { answer: input.answer, expect: input.expect })
    if (action === "end") Object.assign(body, { accept_loss: false, expected_closeability_version: "" })
    try {
      await client._read!(identity, action, body, "action:" + record.request, undefined, { retireUncertain: true })
      record.observed = true
    } catch (error) { record.problem = problemCode(error) }
    this.save(record)
    return this.lookup(record)
  }

  /** This is the pinned encrypted equivalent of receiptPath, with a new read correlation ID. */
  async lookup(record: ActionRecord): Promise<ActionRecord> {
    const client = this.current()
    if (!client || !client.deviceID || client.deviceID !== record.viewer || !client.viewerVerified?.has(record.destination.machineID)) {
      return { ...record, problem: "revoked" }
    }
    const machines = await client.machines().catch(() => null)
    const machine = machines?.machines.find((entry) => entry.id === record.destination.machineID)
    if (!machine || machine.pairing !== "paired") return { ...record, problem: machine ? "revoked" : "unknown" }
    if (!client.allowWrites) return { ...record, problem: "operation_permission" }
    const commands = client.machineDescriptor?.(record.destination.machineID)?.machine?.commands
    if (!client._read || !commands?.includes("session-receipt")) return { ...record, problem: "unsupported" }
    const query = this.requestID()
    try {
      const answer = await client._read({ machine: record.destination.machineID, session: record.destination.sessionID },
        "session-receipt", { request: query, target_request: record.request,
          execution_generation: record.destination.executionGeneration, action: record.action }, "read:" + query)
      const body = ((answer as { body?: unknown })?.body ?? answer) as Partial<ActionReceipt>
      if (body.request !== record.request || body.action !== record.action ||
        body.execution_generation !== record.destination.executionGeneration ||
        !["missing", "pending", "completed", "rejected", "unknown"].includes(String(body.machine_execution))) {
        throw Object.assign(new Error("bad_receipt"), { code: "bad_receipt" })
      }
      const unresolved = ["missing", "pending", "unknown"].includes(String(body.machine_execution))
      const result = { ...record, observed: true, receipt: body as ActionReceipt,
        problem: unresolved ? record.problem : null }
      this.save(result)
      return result
    } catch (error) {
      const result = { ...record, problem: problemCode(error) }
      this.save(result)
      return result
    }
  }

  acknowledge(record: ActionRecord): ActionRecord {
    if (record.receipt?.machine_execution !== "completed" && record.receipt?.machine_execution !== "rejected") {
      throw Object.assign(new Error("receipt_outcome_unknown"), { code: "receipt_outcome_unknown" })
    }
    const result = { ...record, acknowledged: true }
    this.save(result)
    return result
  }
}
