import type { TerminalControl, TerminalFrame } from "@clawdline/contract"
import { bytesBase64 } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load this file in terminal-session.test.ts.
import { completeTerminalFrame, freshTerminalConnection, type TerminalChannelEvent, type TerminalEnvelope } from "./terminal-transport.ts"
import type { TerminalObservation } from "./terminal-observation.js"

export interface TerminalWire {
  subscribeTerminal(connection: string, keyID: string, raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void>
  unsubscribeTerminal(connection: string): void
  publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }>
  observeTerminalFrame(envelope: TerminalEnvelope): void
}

type Receipt = { v: 1; type: "terminal_receipt"; request_id: string; connection: string; operation: string;
  status: "ok" | "refused" | "unknown"; result?: Record<string, unknown>; error?: string }
type Frame = { v: 1; type: "terminal_frame"; terminal_id: string; connection: string; frame_seq: number;
  captured_at: number; frame: TerminalFrame }
type Notice = { v: 1; type: "terminal_notice"; connection: string; code: string; machine_incarnation: string }
export type CloudTerminalHistory = { lines: string[]; truncated: boolean; omitted_lines: number }
export type CloudTerminalState = "opening" | "synchronizing" | "just_synced" | "live" | "stale" | "offline" | "revoked" | "unknown" | "closed"
export interface CloudTerminalSnapshot {
  state: CloudTerminalState
  frame: TerminalFrame | null
  control: TerminalControl | null
  canType: boolean
  hasLease: boolean
  reason: string
}
type Pending = { operation: string; connection: string; terminal: string | null; resolve: (receipt: Receipt) => void; reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout> }
type EarlyFrame = { value: Frame; envelope: TerminalEnvelope }
const STALE_MS = 6_000
const RECEIPT_MS = 10_000
const KEY_MS = 10 * 60_000
export const EARLY_FRAME_LIMIT = 1
const fail = (code: string) => Object.assign(new Error(code), { code })

/** One tab's lease state and one terminal's full-frame stream. No Cloud timeout resends input. */
export class CloudTerminalSession {
  readonly client: string
  private connection = ""
  private keyID = ""
  private expiresAt = 0
  private incarnation = ""
  private terminal = ""
  private frameSeq = 0
  private confirmed = 0
  private epoch: number | null = null
  private nextSeq = 1
  private inputUnknown = false
  private active = false
  private openingNew = false
  private rotationAttempted = false
  private retiringConnection = ""
  private rotationReady = false
  private activating = false
  private lastRenew = 0
  private renewing = false
  private sendQueue: Promise<void> = Promise.resolve()
  private queuedBytes = 0
  private pending = new Map<string, Pending>()
  private earlyFrames: EarlyFrame[] = []
  private listeners = new Set<(snapshot: CloudTerminalSnapshot) => void>()
  private tick: ReturnType<typeof setInterval> | null = null
  private s: CloudTerminalSnapshot = { state: "opening", frame: null, control: null, canType: false, hasLease: false, reason: "" }

  // Declared fields rather than parameter properties: `node --test
  // --experimental-strip-types` cannot run those, and this file's suite silently
  // never ran because of it.
  private readonly transport: TerminalWire
  private readonly observation?: TerminalObservation
  constructor(transport: TerminalWire, clientID: string, observation?: TerminalObservation) {
    this.transport = transport
    this.observation = observation
    this.client = clientID
  }
  get snapshot(): CloudTerminalSnapshot { return this.s }
  subscribe(listener: (snapshot: CloudTerminalSnapshot) => void): () => void {
    this.listeners.add(listener); listener(this.s)
    return () => this.listeners.delete(listener)
  }
  private set(update: Partial<CloudTerminalSnapshot>): void {
    this.s = { ...this.s, ...update }
    this.s.hasLease = this.epoch !== null
    this.s.canType = (this.s.state === "live" || this.s.state === "just_synced") && !!this.s.frame && !!this.s.control?.held &&
      !!this.s.control.holder?.same_client && !!this.s.control.holder?.same_device &&
      this.epoch === this.s.control.epoch && (this.s.control.expires_at ?? 0) * 1000 > Date.now() && !this.inputUnknown &&
      Date.now() / 1000 - this.s.frame.at <= STALE_MS / 1000 && Date.now() < this.expiresAt &&
      !this.openingNew && !this.retiringConnection && this.active
    for (const listener of this.listeners) listener(this.s)
  }

  async start(): Promise<void> {
    if (this.openingNew) throw fail("terminal_busy")
    this.openingNew = true
    const fresh = freshTerminalConnection()
    const previous = this.connection
    const prior = this.s
    const previousExpiry = this.expiresAt
    const previousActive = this.active
    const previousKeyID = this.keyID
    const previousIncarnation = this.incarnation
    const previousFrameSeq = this.frameSeq
    this.earlyFrames = []
    if (previous) { this.retiringConnection = previous; this.rotationReady = false }
    this.active = false
    this.set({ state: previous ? "synchronizing" : "opening", canType: false, reason: "" })
    try {
      await this.transport.subscribeTerminal(fresh.connection, fresh.keyID, fresh.key, (event) => this.event(event))
      this.connection = fresh.connection
      this.keyID = fresh.keyID
      const opened = await this.request(previous && Date.now() < previousExpiry ? "rekey_connection" : "open_connection", {
        key_id: fresh.keyID, key: bytesBase64(fresh.key),
        ...(previous && Date.now() < previousExpiry ? { body: { old_connection: previous } } : {}),
      })
      fresh.key.fill(0)
      if (opened.result?.connection !== fresh.connection || opened.result?.key_id !== fresh.keyID ||
        typeof opened.result?.expires_at !== "number" || opened.result.expires_at * 1000 <= Date.now() ||
        opened.result.expires_at * 1000 > Date.now() + KEY_MS || typeof opened.result?.machine_incarnation !== "string") {
        throw fail("terminal_bad_receipt")
      }
      const sameMachine = !this.incarnation || this.incarnation === opened.result.machine_incarnation
      this.incarnation = opened.result.machine_incarnation
      this.expiresAt = opened.result.expires_at * 1000
      this.frameSeq = 0
      this.active = true
      this.rotationAttempted = false
      this.set({ state: "synchronizing", frame: null })
      if (previous) {
        if (!sameMachine) this.forgetLease("terminal_machine_restarted")
        else await this.reconcileLease()
        this.rotationReady = true
      }
      this.flushEarlyFrame()
      this.maybeActivate()
      if (!this.tick) this.tick = setInterval(() => this.checkFreshness(), 1_000)
    } catch (error) {
      fresh.key.fill(0)
      this.transport.unsubscribeTerminal(fresh.connection)
      this.connection = previous
      this.keyID = previousKeyID
      this.incarnation = previousIncarnation
      this.expiresAt = previousExpiry
      this.frameSeq = previousFrameSeq
      this.earlyFrames = []
      this.retiringConnection = ""
      this.rotationReady = false
      this.active = this.s.state !== "revoked" && previousActive && Date.now() < previousExpiry
      if (this.s.state !== "revoked") {
        this.set({ ...prior, state: previous && Date.now() < previousExpiry ? prior.state : "unknown", reason: (error as Error).message })
      }
      throw error
    } finally {
      this.openingNew = false
      this.set({})
    }
  }

  async request(operation: string, fields: Record<string, unknown> = {}, onRequestID?: (id: string) => void): Promise<Receipt> {
    if (!this.connection) throw fail("terminal_not_connected")
    if (!this.active && operation !== "open_connection" && operation !== "rekey_connection") throw fail("terminal_input_paused")
    if ((operation === "input" || operation === "paste") && !this.s.canType) throw fail("terminal_input_paused")
    const requestID = crypto.randomUUID()
    onRequestID?.(requestID)
    const connection = this.connection
    const receipt = new Promise<Receipt>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestID)
        this.observation?.record("receipt_timeout", { connection, requestID, operation })
        if (operation === "input" || operation === "paste") { this.inputUnknown = true; this.set({ state: "unknown", reason: "terminal_input_state_unknown" }) }
        reject(fail("terminal_receipt_timeout"))
      }, RECEIPT_MS)
      this.pending.set(requestID, { operation, connection,
        terminal: typeof fields.terminal_id === "string" ? fields.terminal_id : null, resolve, reject, timer })
      this.observation?.record("request_pending", { connection, requestID, operation })
    })
    try {
      await this.transport.publishTerminal({ v: 1, type: "terminal_request", request_id: requestID,
        connection, operation, ...fields })
      return await receipt
    } catch (error) {
      const pending = this.pending.get(requestID)
      if (pending) { clearTimeout(pending.timer); this.pending.delete(requestID) }
      if (pending && (operation === "input" || operation === "paste")) {
        this.inputUnknown = true
        this.set({ state: "unknown", reason: "terminal_input_state_unknown" })
      }
      throw error
    }
  }

  async attach(terminal: string): Promise<Receipt> {
    this.terminal = terminal
    this.flushEarlyFrame()
    const [result] = await Promise.all([
      this.request("read", { terminal_id: terminal, client: this.client }),
      this.request("capture", { terminal_id: terminal }),
    ])
    const control = result.result?.control as TerminalControl | undefined
    if (control) this.set({ control })
    return result
  }
  async create(project: string, cols: number, rows: number): Promise<Receipt> {
    const result = await this.request("open", { project_id: project, body: { cols, rows } })
    const id = result.result?.id
    if (typeof id !== "string") throw fail("terminal_bad_receipt")
    this.terminal = id
    this.flushEarlyFrame()
    const control = result.result?.control as TerminalControl | undefined
    if (control) this.set({ control })
    await this.request("capture", { terminal_id: id })
    return result
  }
  async acquire(action: "acquire" | "takeover"): Promise<void> {
    const receipt = await this.request("control", { terminal_id: this.terminal, client: this.client, body: { action } })
    const control = (receipt.result?.control ?? receipt.result) as TerminalControl | undefined
    if (!control?.held || !control.holder?.same_client) throw fail("not_controller")
    this.epoch = control.epoch
    this.confirmed = control.applied_through ?? 0
    this.nextSeq = this.confirmed + 1
    this.inputUnknown = false
    this.lastRenew = Date.now()
    this.frameSeq = 0
    this.set({ control, frame: null, state: "synchronizing", reason: "" })
    await this.request("capture", { terminal_id: this.terminal })
  }
  async reconcileLease(): Promise<void> {
    if (this.epoch === null || !this.terminal) return
    const receipt = await this.request("control", { terminal_id: this.terminal, client: this.client })
    const control = receipt.result?.control as TerminalControl | undefined
    if (receipt.result?.machine_incarnation !== this.incarnation || receipt.result?.input_state_unknown === true ||
      !control?.held || !control.holder?.same_client || !control.holder?.same_device ||
      control.epoch !== this.epoch || (control.applied_through ?? -1) < this.confirmed ||
      typeof control.expires_at !== "number" || control.expires_at * 1000 <= Date.now()) {
      this.forgetLease("terminal_input_state_unknown")
      return
    }
    this.confirmed = control.applied_through ?? 0
    this.nextSeq = this.confirmed + 1
    this.inputUnknown = false
    this.lastRenew = Date.now()
    this.set({ control })
  }
  input(data: Uint8Array): Promise<void> {
    if (data.length > 4096 || this.queuedBytes + data.length > 65536) return Promise.reject(fail("input_too_large"))
    this.queuedBytes += data.length
    const result = this.sendQueue.then(() => this.inputNow(data)).finally(() => { this.queuedBytes -= data.length })
    this.sendQueue = result.catch(() => undefined)
    return result
  }
  private async inputNow(data: Uint8Array): Promise<void> {
    if (!this.s.canType || this.epoch === null) throw fail("terminal_input_paused")
    const seq = this.nextSeq
    const receipt = await this.request("input", { terminal_id: this.terminal, client: this.client,
      epoch: this.epoch, seq, body: { data: bytesBase64(data) } })
    this.applied(receipt, seq)
  }
  paste(text: string): Promise<void> {
    const size = new TextEncoder().encode(text).length
    if (size > 65536 || this.queuedBytes + size > 65536) return Promise.reject(fail("input_too_large"))
    this.queuedBytes += size
    const result = this.sendQueue.then(() => this.pasteNow(text)).finally(() => { this.queuedBytes -= size })
    this.sendQueue = result.catch(() => undefined)
    return result
  }
  private async pasteNow(text: string): Promise<void> {
    if (!this.s.canType || this.epoch === null) throw fail("terminal_input_paused")
    const seq = this.nextSeq
    const receipt = await this.request("paste", { terminal_id: this.terminal, client: this.client,
      epoch: this.epoch, seq, body: { text } })
    this.applied(receipt, seq)
  }
  private applied(receipt: Receipt, seq: number): void {
    if (receipt.result?.applied_through !== seq) {
      this.inputUnknown = true
      this.set({ state: "unknown", reason: "terminal_input_state_unknown" })
      throw fail("terminal_input_state_unknown")
    }
    this.confirmed = seq
    this.nextSeq = seq + 1
  }
  async release(): Promise<void> {
    await this.request("control", { terminal_id: this.terminal, client: this.client, body: { action: "release" } })
    this.epoch = null
    this.set({ control: null })
  }
  async resize(cols: number, rows: number): Promise<void> {
    if (!this.s.canType || this.epoch === null) throw fail("not_controller")
    await this.request("resize", { terminal_id: this.terminal, client: this.client, epoch: this.epoch, body: { cols, rows } })
  }
  async close(onRequestID?: (id: string) => void): Promise<void> {
    if (!this.s.canType || this.epoch === null) throw fail("not_controller")
    await this.request("close", { terminal_id: this.terminal, client: this.client, epoch: this.epoch }, onRequestID)
    this.set({ state: "closed" })
  }
  async history(): Promise<CloudTerminalHistory> {
    const receipt = await this.request("history", { terminal_id: this.terminal, body: { lines: 2000 } })
    const lines = receipt.result?.lines
    const truncated = receipt.result?.truncated
    const omitted = receipt.result?.omitted_lines
    if (!Array.isArray(lines) || !lines.every((line) => typeof line === "string") || typeof truncated !== "boolean" ||
      !Number.isSafeInteger(omitted) || (omitted as number) < 0 || truncated !== ((omitted as number) > 0)) throw fail("terminal_bad_receipt")
    return { lines, truncated, omitted_lines: omitted as number }
  }
  private forgetLease(reason: string): void { this.epoch = null; this.inputUnknown = true; this.set({ control: null, state: "unknown", reason }) }
  private revoke(reason: string): void {
    this.epoch = null
    this.inputUnknown = true
    this.active = false
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer)
      pending.reject(fail("terminal_access_revoked"))
    }
    this.pending.clear()
    this.set({ control: null, state: "revoked", reason })
  }
  private event(event: TerminalChannelEvent): void {
    if ("error" in event) {
      if (this.s.state === "revoked") return
      this.inputUnknown = true
      if (event.error === "machine_offline" || event.error === "machine_stale") this.set({ state: "offline", reason: event.error })
      else if (event.error.includes("revoked") || event.error === "terminal_forbidden" || event.error === "forbidden") this.revoke(event.error)
      else this.set({ state: "unknown", reason: event.error })
      return
    }
    const value = event.plaintext as Receipt | Frame | Notice
    if (value?.v !== 1 || value.connection !== this.connection) {
      if (value?.type === "terminal_receipt") this.observation?.record("pending_miss", {
        connection: typeof value.connection === "string" ? value.connection : undefined,
        requestID: typeof value.request_id === "string" ? value.request_id : undefined,
        operation: typeof value.operation === "string" ? value.operation : undefined,
        code: value.v !== 1 ? "version_mismatch" : "connection_mismatch",
      })
      return
    }
    if (this.s.state === "revoked" && value.type !== "terminal_notice") return
    if (value.type === "terminal_notice") {
      if (!event.envelope.ch.startsWith("termr/") || !event.envelope.ch.endsWith("/" + this.connection) ||
        value.code !== "terminal_access_revoked" || typeof value.machine_incarnation !== "string" ||
        !value.machine_incarnation || "request_id" in value) return
      this.revoke(value.machine_incarnation === this.incarnation ? "terminal_access_revoked" : "terminal_machine_restarted")
    } else if (value.type === "terminal_receipt") {
      if (!event.envelope.ch.startsWith("termr/")) {
        this.observation?.record("pending_miss", { connection: value.connection, requestID: value.request_id,
          operation: value.operation, code: "channel_mismatch" })
        return
      }
      const pending = this.pending.get(value.request_id)
      const mismatch = !pending ? "request_unknown" : pending.connection !== value.connection ? "connection_mismatch" :
        pending.operation !== value.operation ? "operation_mismatch" :
          pending.terminal && (value as Receipt & { terminal_id?: string }).terminal_id !== pending.terminal ? "terminal_mismatch" : ""
      if (!pending || mismatch) {
        this.observation?.record("pending_miss", { connection: value.connection, requestID: value.request_id,
          operation: value.operation, code: mismatch })
        return
      }
      this.observation?.record("pending_match", { connection: value.connection, requestID: value.request_id,
        operation: value.operation })
      clearTimeout(pending.timer); this.pending.delete(value.request_id)
      this.observation?.record("session_settled", { connection: value.connection, requestID: value.request_id,
        operation: value.operation })
      if (value.status === "ok") pending.resolve(value)
      else {
        if (value.status === "unknown") this.forgetLease("terminal_input_state_unknown")
        else if (["terminal_forbidden", "terminal_access_revoked"].includes(value.error ?? "")) this.revoke(value.error ?? "terminal_forbidden")
        else if (value.error === "terminal_closed") this.set({ state: "closed", reason: value.error })
        else if (pending.operation === "input" || pending.operation === "paste") {
          this.inputUnknown = true
          this.set({ state: "unknown", reason: value.error ?? "terminal_input_paused" })
        }
        pending.reject(Object.assign(fail(value.error ?? (value.status === "unknown" ? "terminal_result_unknown" : "terminal_forbidden")),
          { receiptStatus: value.status }))
      }
    } else if (value.type === "terminal_frame" &&
      event.envelope.ch.startsWith("term/") &&
      Number.isSafeInteger(value.frame_seq) && value.frame_seq > 0 &&
      typeof value.captured_at === "number" && Math.abs(Date.now() / 1000 - value.captured_at) <= STALE_MS / 1000 &&
      completeTerminalFrame(value.frame)) {
      if (!this.active || this.openingNew || !this.terminal) {
        if (this.earlyFrames.length < EARLY_FRAME_LIMIT) this.earlyFrames.push({ value, envelope: event.envelope })
        else if (value.frame_seq > this.earlyFrames[0].value.frame_seq) {
          this.earlyFrames.splice(0, EARLY_FRAME_LIMIT, { value, envelope: event.envelope })
        }
      } else this.acceptFrame(value, event.envelope)
    }
  }
  private flushEarlyFrame(): void {
    if (!this.active || !this.terminal || !this.earlyFrames.length) return
    const early = this.earlyFrames.shift()!
    this.acceptFrame(early.value, early.envelope)
  }
  private acceptFrame(value: Frame, envelope: TerminalEnvelope): void {
    if (value.connection !== this.connection || value.terminal_id !== this.terminal ||
      value.frame_seq <= this.frameSeq || Math.abs(Date.now() / 1000 - value.captured_at) > STALE_MS / 1000 ||
      !completeTerminalFrame(value.frame)) return
    const first = this.frameSeq === 0
    this.frameSeq = value.frame_seq
    this.set({ frame: value.frame, state: value.frame.dead ? "closed" : first ? "just_synced" : "live", reason: "" })
    this.transport.observeTerminalFrame(envelope)
    this.maybeActivate()
  }
  private maybeActivate(): void {
    if (!this.retiringConnection || !this.rotationReady || !this.frameSeq || this.activating ||
      !this.s.frame || this.s.state === "closed") return
    this.activating = true
    const old = this.retiringConnection
    const current = this.connection
    void this.request("activate_connection", { body: { old_connection: old, first_frame_seq: this.frameSeq } })
      .then((receipt) => {
        if (current !== this.connection || receipt.result?.connection !== current || receipt.result?.retired_connection !== old) {
          throw fail("terminal_bad_receipt")
        }
        this.transport.unsubscribeTerminal(old)
        this.retiringConnection = ""
        this.rotationReady = false
        this.set({})
      })
      .catch((error) => { this.inputUnknown = true; this.set({ state: "unknown", reason: (error as Error).message }) })
      .finally(() => { this.activating = false })
  }
  private checkFreshness(): void {
    if (this.s.state === "revoked") return
    if (this.epoch !== null && this.active && !this.openingNew && !this.retiringConnection &&
      !this.inputUnknown && !this.renewing && Date.now() - this.lastRenew >= 10_000) {
      this.renewing = true
      this.lastRenew = Date.now()
      void this.request("control", { terminal_id: this.terminal, client: this.client, body: { action: "renew" } })
        .then((receipt) => {
          const control = (receipt.result?.control ?? receipt.result) as TerminalControl | undefined
          if (!control?.held || !control.holder?.same_client || !control.holder?.same_device ||
            control.epoch !== this.epoch || (control.expires_at ?? 0) * 1000 <= Date.now() ||
            (control.applied_through ?? -1) < this.confirmed) this.forgetLease("terminal_input_state_unknown")
          else this.set({ control })
        })
        .catch(() => this.forgetLease("terminal_input_state_unknown"))
        .finally(() => { this.renewing = false })
    }
    if (this.connection && !this.openingNew && !this.rotationAttempted && this.expiresAt - Date.now() < 60_000 && Date.now() < this.expiresAt) {
      this.rotationAttempted = true
      void this.start().catch(() => undefined)
    }
    if (Date.now() >= this.expiresAt || (this.s.frame && Date.now() / 1000 - this.s.frame.at > STALE_MS / 1000)) {
      this.set({ state: "stale", reason: "terminal_stale" })
    }
  }
  dispose(): void {
    this.earlyFrames = []
    if (this.tick) clearInterval(this.tick)
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(fail("terminal_closed")) }
    this.pending.clear()
    if (this.connection) this.transport.unsubscribeTerminal(this.connection)
    if (this.retiringConnection) this.transport.unsubscribeTerminal(this.retiringConnection)
    this.set({ state: "closed" })
    this.listeners.clear()
  }
}
