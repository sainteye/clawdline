import type { TerminalControl, TerminalFrame } from "@clawdline/contract"
import { bytesBase64 } from "../legacy/js/net/cloud-crypto.js"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load this file in terminal-session.test.ts.
import { completeTerminalFrame, freshTerminalConnection, type TerminalCarrier, type TerminalChannelEvent, type TerminalDirectOffer, type TerminalEnvelope } from "./terminal-transport.ts"
import type { TerminalObservation } from "./terminal-observation.js"
// @ts-expect-error -- Node's strip-types runner loads this module directly.
import { reconstructTerminalDelta, type TerminalDelta } from "./terminal-delta.ts"
// @ts-expect-error -- Node's strip-types runner loads this module directly.
import { DIRECT_BUSY_RETRY_MS, DIRECT_RETRY_MS } from "./terminal-direct.ts"

export interface TerminalWire {
  subscribeTerminal(connection: string, keyID: string, raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void>
  unsubscribeTerminal(connection: string): void
  publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }>
  observeTerminalFrame(envelope: TerminalEnvelope): void
  deltaAvailable?(connection: string): boolean
  /** The direct carrier; a wire without these keeps every connection on the relay. */
  directSupported?(): boolean
  prepareDirect?(connection: string, onDown: (code: string) => void): Promise<TerminalDirectOffer>
  setCarrier?(connection: string, carrier: TerminalCarrier): void
}

type Receipt = { v: 1; type: "terminal_receipt"; request_id: string; connection: string; operation: string;
  status: "ok" | "refused" | "unknown"; result?: Record<string, unknown>; error?: string }
type Frame = { v: 1; type: "terminal_frame"; terminal_id: string; connection: string; frame_seq: number;
  captured_at: number; frame: TerminalFrame }
type DeltaFrame = TerminalDelta
type Notice = { v: 1; type: "terminal_notice"; connection: string; code: string; machine_incarnation: string }
export type CloudTerminalHistory = { lines: string[]; truncated: boolean; omitted_lines: number }
export type CloudTerminalState = "opening" | "synchronizing" | "just_synced" | "live" | "stale" | "offline" | "revoked" | "unknown" | "closed"
export interface CloudTerminalSnapshot {
  state: CloudTerminalState
  frame: TerminalFrame | null
  control: TerminalControl | null
  canType: boolean
  /**
   * Typing is paused only for a moment this tab expects to end by itself: it holds the lease and is
   * opening, synchronizing or moving to another connection (the direct path). Keys typed now wait in
   * the tab, never sent, and go out once typing resumes, or are dropped after TYPE_AHEAD_MS.
   */
  typeAhead?: boolean
  hasLease: boolean
  reason: string
  /** How the current connection's screens and requests travel. */
  carrier: TerminalCarrier
}
type Pending = { operation: string; connection: string; terminal: string | null; resolve: (receipt: Receipt) => void; reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout> }
type EarlyFrame = { value: Frame; envelope: TerminalEnvelope }
/** No verified frame has arrived for this long, by this browser's own clock: the screen is stale. */
const STALE_MS = 6_000
const RECEIPT_MS = 10_000
const RENEW_MS = 10_000
/** CloudTerminalRenewRetrySecondsLimit: a renewal that went unanswered or was refused for load is asked again this soon. */
export const RENEW_RETRY_MS = 2_000
/** CloudTerminalRotationRetrySecondsLimit: a key rotation that failed is tried again this soon while the old key lasts. */
export const ROTATION_RETRY_MS = 5_000
/** Refusals that say the machine or relay was briefly too busy, not that the lease is gone. */
const TRANSIENT = new Set(["terminal_busy", "rate_limited", "over_capacity", "terminal_direct_closed", "cloud_reconnecting"])
/** CloudTerminalTypeAheadSecondsLimit: how long a key typed during a brief pause waits for typing to resume. */
export const TYPE_AHEAD_MS = 8_000
const KEY_MS = 10 * 60_000
/** An upgrade is not started this close to the key rotation, which would race it. */
const UPGRADE_MIN_KEY_MS = 120_000
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
  private deltaEnabled = false
  private deltaChecking = false
  private confirmed = 0
  private epoch: number | null = null
  private nextSeq = 1
  /** Control is being taken right now (on entry, or by the person's own button): keys typed meanwhile wait. */
  private controlPending = 0
  private inputUnknown = false
  private connectionUnknown = false
  private active = false
  private openingNew = false
  private rotationAttempted = false
  private rotationRetryAt = 0
  private retiringConnection = ""
  private rotationReady = false
  private activating = false
  private lastRenew = 0
  private renewing = false
  /** The request id of the renewal in flight: its loss is retried, never read as a lost lease. */
  private renewID = ""
  /** When this browser last drew a verified frame. Staleness is judged by it, not by the machine's clock. */
  private frameSeenAt = 0
  private carrier: TerminalCarrier = "relay"
  /** The open peer of the current direct connection, or of an upgrade in progress. */
  private direct: TerminalDirectOffer | null = null
  private directAttempt: object | null = null
  private upgrading = false
  private fallbackWanted = false
  /** The machine offered, in its direct answer, to send everyday receipts on the DC. */
  private directReceipts = false
  private readonly directRetryAt = new Map<string, number>()
  private sendQueue: Promise<void> = Promise.resolve()
  private inFlightInput: Promise<void>[] = []
  private queuedBytes = 0
  private pending = new Map<string, Pending>()
  private earlyFrames: EarlyFrame[] = []
  private listeners = new Set<(snapshot: CloudTerminalSnapshot) => void>()
  private tick: ReturnType<typeof setInterval> | null = null
  private s: CloudTerminalSnapshot = { state: "opening", frame: null, control: null, canType: false, hasLease: false, reason: "", carrier: "relay" }

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
  /** A retained connection may serve another read while its key and identity are valid. */
  get reusable(): boolean { return this.active && !this.openingNew && !this.retiringConnection &&
    !this.inputUnknown && !this.connectionUnknown && this.s.state !== "revoked" && this.s.state !== "unknown" && this.s.state !== "offline" &&
    Date.now() < this.expiresAt - 60_000 }
  get releasable(): boolean { return this.active && !this.openingNew && !!this.connection && this.s.state !== "revoked" && Date.now() < this.expiresAt }
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
      Date.now() - this.frameSeenAt <= STALE_MS && Date.now() < this.expiresAt &&
      !this.openingNew && !this.retiringConnection && this.active
    // A stale screen whose key still lasts is waiting for its next frame (a late heartbeat), not lost.
    this.s.typeAhead = !this.s.canType && this.epoch !== null && !this.inputUnknown &&
      (this.s.state === "opening" || this.s.state === "synchronizing" || this.s.state === "live" || this.s.state === "just_synced" ||
        (this.s.state === "stale" && Date.now() < this.expiresAt)) &&
      (!this.s.control || (!!this.s.control.held && !!this.s.control.holder?.same_client && this.epoch === this.s.control.epoch))
    // A tab still taking control is about to hold it: a key typed now waits for the answer rather than
    // being refused, and is refused with that answer's reason if control does not come.
    if (!this.s.canType && !this.s.typeAhead && this.controlPending > 0 && !this.inputUnknown &&
      (this.s.state === "opening" || this.s.state === "synchronizing" || this.s.state === "live" || this.s.state === "just_synced")) this.s.typeAhead = true
    for (const listener of this.listeners) listener(this.s)
  }

  /**
   * Opens this tab's connection, or rekeys the current one. `carrier: "direct"` rekeys it onto the
   * open DC; a direct connection's own rotation stays direct while its DC is open. `abandon` names a
   * current connection the machine has already retired: a new one opens and proves the lease.
   */
  async start(options: { carrier?: "direct"; abandon?: boolean } = {}): Promise<void> {
    if (this.openingNew) throw fail("terminal_busy")
    const previous = this.connection
    const rekey = !!previous && Date.now() < this.expiresAt && !options.abandon
    if (options.carrier === "direct" && (!rekey || !this.direct?.open)) throw fail("terminal_direct_unavailable")
    const direct = rekey && !!this.direct?.open && (options.carrier === "direct" || this.carrier === "direct")
    this.openingNew = true
    const fresh = freshTerminalConnection()
    const previousCarrier = this.carrier
    const prior = this.s
    const previousExpiry = this.expiresAt
    const previousActive = this.active
    const previousKeyID = this.keyID
    const previousIncarnation = this.incarnation
    const previousFrameSeq = this.frameSeq
    // A rotation that never activated (a direct connection retired before its first frame, say)
    // leaves an older connection still subscribed; the new rotation replaces it.
    const previousRetiring = this.retiringConnection
    const previousRotationReady = this.rotationReady
    this.earlyFrames = []
    if (previous && !options.abandon) { this.retiringConnection = previous; this.rotationReady = false }
    else if (options.abandon) { this.retiringConnection = ""; this.rotationReady = false }
    this.active = false
    this.set({ state: previous ? "synchronizing" : "opening", canType: false, reason: "" })
    try {
      await this.transport.subscribeTerminal(fresh.connection, fresh.keyID, fresh.key, (event) => this.event(event))
      const requestDelta = this.transport.deltaAvailable?.(fresh.connection) === true
      this.connection = fresh.connection
      this.keyID = fresh.keyID
      const opened = await this.request(rekey ? "rekey_connection" : "open_connection", {
        key_id: fresh.keyID, key: bytesBase64(fresh.key),
        ...(rekey || requestDelta ? {
          body: { ...(rekey ? { old_connection: previous } : {}), ...(direct ? { carrier: "direct" } : {}),
            ...(direct && this.directReceipts ? { direct_receipts: true } : {}),
            ...(requestDelta ? { frame_delta_v1: true } : {}) },
        } : {}),
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
      this.deltaEnabled = requestDelta && opened.result.frame_delta_v1 === true
      // Direct only when the machine's receipt says so; it never silently downgrades.
      const carrier: TerminalCarrier = direct && opened.result.carrier === "direct" ? "direct" : "relay"
      this.transport.setCarrier?.(fresh.connection, carrier)
      if (carrier !== previousCarrier) this.observation?.record("carrier_changed", { connection: fresh.connection, code: carrier })
      this.carrier = carrier
      if (carrier === "direct" && !this.direct?.open) this.fallbackWanted = true
      if (options.abandon && previous) this.transport.unsubscribeTerminal(previous)
      if (previousRetiring && previousRetiring !== previous) this.transport.unsubscribeTerminal(previousRetiring)
      this.active = true
      this.connectionUnknown = false
      this.rotationAttempted = false
      this.set({ state: "synchronizing", frame: null, carrier })
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
      this.deltaEnabled = false
      this.earlyFrames = []
      this.retiringConnection = previousRetiring
      this.rotationReady = previousRotationReady
      this.carrier = previousCarrier
      // A direct connection whose rotation failed falls back to the relay rather than expire.
      if (direct && !options.carrier && previousCarrier === "direct") this.fallbackWanted = true
      this.active = this.s.state !== "revoked" && previousActive && Date.now() < previousExpiry
      if (this.s.state !== "revoked") {
        this.set({ ...prior, state: previous && Date.now() < previousExpiry ? prior.state : "unknown", reason: (error as Error).message })
      }
      throw error
    } finally {
      this.openingNew = false
      this.set({})
      if (this.fallbackWanted) { this.fallbackWanted = false; queueMicrotask(() => void this.fallback()) }
    }
  }

  /**
   * Upgrades the current relay connection to a direct one when nothing else is in motion. Only a tab
   * holding the terminal's lease upgrades: the machine activates a rekeyed connection only for its holder.
   */
  private maybeUpgrade(): void {
    if (!this.transport.directSupported?.() || !this.transport.prepareDirect || this.upgrading || this.carrier !== "relay" ||
      this.epoch === null ||
      !this.active || this.openingNew || this.retiringConnection || this.activating || !this.terminal || !this.frameSeq ||
      (this.s.state !== "live" && this.s.state !== "just_synced") || this.expiresAt - Date.now() < UPGRADE_MIN_KEY_MS ||
      Date.now() < (this.directRetryAt.get(this.terminal) ?? 0)) return
    void this.upgrade()
  }
  private async upgrade(): Promise<void> {
    this.upgrading = true
    const attempt = {}
    this.directAttempt = attempt
    const connection = this.connection
    const terminal = this.terminal
    let offer: TerminalDirectOffer | null = null
    try {
      offer = await this.transport.prepareDirect!(connection, (code) => this.directDown(attempt, code))
      if (this.directAttempt !== attempt || connection !== this.connection || !this.active) throw fail("terminal_direct_stale")
      const answer = await this.request("direct_offer", { body: { sealed: offer.sealed } })
      const sdp = answer.result?.sdp
      if (typeof sdp !== "string" || !sdp) throw fail("terminal_bad_receipt")
      this.directReceipts = answer.result?.direct_receipts === true
      await offer.answer(sdp)
      if (this.directAttempt !== attempt || connection !== this.connection || this.openingNew || this.retiringConnection || !this.active)
        throw fail("terminal_direct_stale")
      this.direct = offer
      await this.start({ carrier: "direct" })
      if (this.carrier !== "direct") throw fail("terminal_direct_unavailable")
    } catch (error) {
      // A machine without the direct path (`terminal_invalid`), or refusing it now
      // (`terminal_direct_disabled`, `terminal_direct_unavailable`), leaves the terminal on the relay silently.
      const code = (error as { code?: string })?.code ?? "terminal_direct_failed"
      this.observation?.record("direct_failed", { connection, code })
      // The relay's account budget or its two-connection rotation room is briefly full
      // while a page opens; that passes in seconds, unlike a machine's refusal.
      const busy = code === "rate_limited" || code === "over_capacity"
      this.directRetryAt.set(terminal, Date.now() + (busy ? DIRECT_BUSY_RETRY_MS : DIRECT_RETRY_MS))
      if (this.directAttempt === attempt) { this.directAttempt = null; this.direct = null }
      offer?.close()
    } finally { this.upgrading = false }
  }
  /** The DC is gone: a direct connection goes back to the relay, without replaying any input. */
  private directDown(attempt: object, _code: string): void {
    if (this.directAttempt !== attempt) return
    this.directAttempt = null
    this.direct = null
    if (this.terminal) this.directRetryAt.set(this.terminal, Date.now() + DIRECT_RETRY_MS)
    if (this.carrier === "direct") void this.fallback()
  }
  private closeDirect(): void {
    const offer = this.direct
    this.direct = null
    this.directAttempt = null
    offer?.close()
  }
  private async fallback(): Promise<void> {
    if (this.carrier !== "direct" || !this.connection || this.s.state === "revoked" || this.s.state === "closed") return
    if (this.openingNew) { this.fallbackWanted = true; return }
    this.closeDirect()
    try { await this.start() } // A relay rekey naming the direct connection as its old one.
    catch (error) {
      // Re-read after the await: the receipt may have revoked access or started another connection.
      const now = this.s.state as CloudTerminalState
      if (now === "revoked" || this.carrier !== "direct" || this.openingNew) return
      if ((error as { receiptStatus?: string })?.receiptStatus !== "refused") return
      // The machine already retired it with its peer: a new connection proves the lease instead.
      try {
        await this.start({ abandon: true })
        if (this.terminal) await this.attach(this.terminal, false)
      } catch (failure) {
        if ((this.s.state as CloudTerminalState) !== "revoked") this.set({ state: "unknown", reason: (failure as Error).message })
      }
    }
  }

  private issueRequest(operation: string, fields: Record<string, unknown> = {}, onRequestID?: (id: string) => void): { sent: Promise<void>; receipt: Promise<Receipt> } {
    if (!this.connection) throw fail("terminal_not_connected")
    if (!this.active && operation !== "open_connection" && operation !== "rekey_connection" && operation !== "release_connection") throw this.paused()
    if ((operation === "input" || operation === "paste") && !this.s.canType) throw this.paused()
    const requestID = crypto.randomUUID()
    onRequestID?.(requestID)
    const connection = this.connection
    const receipt = new Promise<Receipt>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestID)
        this.observation?.record("receipt_timeout", { connection, requestID, operation })
        if (operation === "input" || operation === "paste") { this.inputUnknown = true; this.set({ state: "unknown", reason: "terminal_input_state_unknown" }) }
        // A renewal changes nothing the browser cannot ask again; the next one is due shortly.
        else if (requestID === this.renewID) { /* retried by checkFreshness */ }
        else if (operation !== "release_connection" && operation !== "direct_offer") { this.connectionUnknown = true; this.set({ state: "unknown", reason: "terminal_receipt_timeout" }) }
        // A key with no receipt may or may not have been typed (the machine drops an envelope it
        // takes for a replay without a word): it is refused as unknown, with the way back.
        reject(fail(operation === "input" || operation === "paste" ? "terminal_input_state_unknown" : "terminal_receipt_timeout"))
      }, RECEIPT_MS)
      this.pending.set(requestID, { operation, connection,
        terminal: typeof fields.terminal_id === "string" ? fields.terminal_id : null, resolve, reject, timer })
      this.observation?.record("request_pending", { connection, requestID, operation })
    })
    const sent = this.transport.publishTerminal({ v: 1, type: "terminal_request", request_id: requestID,
        connection, operation, ...fields })
      .then(() => undefined, (error) => {
      const pending = this.pending.get(requestID)
      if (pending) { clearTimeout(pending.timer); this.pending.delete(requestID); pending.reject(error instanceof Error ? error : fail("terminal_send_failed")) }
      // The transport refuses a direct connection's request once its DC is no longer open, before
      // the DC's close event: nothing was sent (a partly sent message dies with the DC, and the
      // machine discards it), so the outcome is known, and the connection falls back now.
      const unsent = (error as { code?: string })?.code === "terminal_direct_closed"
      if (unsent && connection === this.connection) this.directClosing()
      if (pending && !unsent && (operation === "input" || operation === "paste")) {
        this.inputUnknown = true
        this.set({ state: "unknown", reason: "terminal_input_state_unknown" })
      }
      throw error
      })
    // A publication failure rejects both paths; the caller may still be
    // awaiting `sent` when the receipt promise is rejected.
    void receipt.catch(() => undefined)
    return { sent, receipt }
  }

  async request(operation: string, fields: Record<string, unknown> = {}, onRequestID?: (id: string) => void): Promise<Receipt> {
    const issued = this.issueRequest(operation, fields, onRequestID)
    await issued.sent
    return issued.receipt
  }

  private async captureOrStream(terminal: string): Promise<void> {
    try { await this.request("capture", { terminal_id: terminal }) }
    catch (error) {
      if ((error as { code?: unknown })?.code !== "rate_limited") throw error
    }
  }

  async attach(terminal: string, reset = true): Promise<Receipt> {
    if (reset || this.terminal !== terminal) {
      // The same terminal on the same connection keeps its drawn screen: it is the base the
      // machine's next delta is built on (see acquireNow).
      const keep = this.terminal === terminal && this.frameSeq > 0
      this.terminal = ""
      this.epoch = null
      if (!keep) this.frameSeq = 0
      this.set({ ...(keep ? {} : { frame: null }), control: null, state: "synchronizing", reason: "" })
    }
    this.terminal = terminal
    this.flushEarlyFrame()
    const [result] = await Promise.all([
      this.request("read", { terminal_id: terminal, client: this.client }),
      // A read already starts the screen watch. If the relay's short grant
      // refuses this extra capture, its next verified frame still arrives on
      // the held channel; the viewer must retain the successful read/control.
      this.captureOrStream(terminal),
    ])
    const control = result.result?.control as TerminalControl | undefined
    if (control) this.set({ control })
    return result
  }
  async create(project: string, cols: number, rows: number): Promise<Receipt> {
    const result = await this.request("open", { project_id: project, body: { cols, rows } })
    const id = result.result?.id
    if (typeof id !== "string") throw fail("terminal_bad_receipt")
    this.epoch = null
    this.frameSeq = 0
    this.set({ frame: null, control: null, state: "synchronizing", reason: "" })
    this.terminal = id
    this.flushEarlyFrame()
    const control = result.result?.control as TerminalControl | undefined
    if (control) this.set({ control })
    await this.captureOrStream(id)
    return result
  }
  /**
   * Runs `work`, the page's own taking of control as it opens a terminal, with keys typed meanwhile
   * held as type-ahead instead of refused for want of a lease that is on its way.
   */
  async expectControl<T>(work: () => Promise<T>): Promise<T> {
    this.controlPending++
    this.set({})
    try { return await work() } finally { this.controlPending--; this.set({}) }
  }
  acquire(action: "acquire" | "takeover"): Promise<void> {
    return this.expectControl(() => this.acquireNow(action))
  }
  private async acquireNow(action: "acquire" | "takeover"): Promise<void> {
    const receipt = await this.request("control", { terminal_id: this.terminal, client: this.client, body: { action } })
    const control = (receipt.result?.control ?? receipt.result) as TerminalControl | undefined
    if (!control?.held || !control.holder?.same_client) throw fail("not_controller")
    this.epoch = control.epoch
    this.confirmed = control.applied_through ?? 0
    this.nextSeq = this.confirmed + 1
    this.inputUnknown = false
    this.lastRenew = Date.now()
    // Typing waits for the next verified screen, but the one drawn stays as the base of the machine's
    // next delta: the machine keeps its own base across a lease change, and a tab that dropped its
    // copy read that delta as corrupt and threw the connection away (after 接手, 2026-10-06).
    this.set({ control, state: "synchronizing", reason: "" })
    await this.captureOrStream(this.terminal)
  }
  async reconcileLease(): Promise<void> {
    if (this.epoch === null || !this.terminal) return
    const receipt = await this.request("control", { terminal_id: this.terminal, client: this.client })
    const control = receipt.result?.control as TerminalControl | undefined
    if (receipt.result?.machine_incarnation !== this.incarnation || receipt.result?.input_state_unknown === true ||
      !control?.held || !control.holder?.same_client || !control.holder?.same_device ||
      control.epoch !== this.epoch || (control.applied_through ?? 0) < this.confirmed ||
      typeof control.expires_at !== "number" || control.expires_at * 1000 <= Date.now()) {
      this.forgetLease("terminal_input_state_unknown")
      return
    }
    this.confirmed = control.applied_through ?? 0
    // A key published before the rekey may still be on its way; its number is never handed out again.
    this.nextSeq = Math.max(this.nextSeq, this.confirmed + 1)
    this.inputUnknown = false
    this.lastRenew = Date.now()
    this.set({ control })
  }
  input(data: Uint8Array): Promise<void> {
    if (data.length > 4096 || this.queuedBytes + data.length > 65536) return Promise.reject(fail("input_too_large"))
    this.queuedBytes += data.length
    return this.queueInput("input", { data: bytesBase64(data) }, data.length)
  }
  paste(text: string): Promise<void> {
    const size = new TextEncoder().encode(text).length
    if (size > 65536 || this.queuedBytes + size > 65536) return Promise.reject(fail("input_too_large"))
    this.queuedBytes += size
    return this.queueInput("paste", { text }, size)
  }
  private queueInput(operation: "input" | "paste", body: Record<string, unknown>, size: number): Promise<void> {
    let settle!: (error?: unknown) => void
    const result = new Promise<void>((resolve, reject) => { settle = (error) => error ? reject(error) : resolve() })
    const send = this.sendQueue.then(async () => {
      // Bound unacknowledged inputs while allowing several keystrokes to cross
      // a high-latency link without a full receipt round trip between them.
      if (this.inFlightInput.length >= 4) await this.inFlightInput[0]!.catch(() => undefined)
      for (;;) {
        await this.untilTypeable()
        if (!this.s.canType || this.epoch === null) throw this.paused()
        const seq = this.nextSeq++
        let issued: { sent: Promise<void>; receipt: Promise<Receipt> }
        try {
          issued = this.issueRequest(operation, { terminal_id: this.terminal, client: this.client, epoch: this.epoch, seq, body })
          await issued.sent
        } catch (error) {
          // Never published (see issueRequest): the same numbered key waits for the relay connection.
          // Were it somehow applied, the machine's high-water mark for this epoch and seq absorbs it.
          if ((error as { code?: string })?.code !== "terminal_direct_closed") throw error
          if (this.nextSeq === seq + 1) this.nextSeq = seq
          continue
        }
        const settled = issued.receipt.then((receipt) => {
          try { this.applied(receipt, seq); settle() } catch (error) { settle(error) }
        }, settle)
        this.inFlightInput.push(settled)
        void settled.finally(() => {
          const at = this.inFlightInput.indexOf(settled)
          if (at >= 0) this.inFlightInput.splice(at, 1)
        })
        return
      }
    })
    this.sendQueue = send.catch(() => undefined)
    void send.catch(settle)
    return result.finally(() => { this.queuedBytes -= size })
  }
  /** Waits out a brief pause (`typeAhead`); a key typed then was never sent, so sending it later replays nothing. */
  private untilTypeable(): Promise<void> {
    if (this.s.canType && this.epoch !== null) return Promise.resolve()
    if (!this.s.typeAhead) return Promise.reject(this.paused())
    return new Promise<void>((resolve, reject) => {
      const done = () => { clearTimeout(timer); this.listeners.delete(check) }
      // Only this refusal is the pause itself outlasting its bound; every other one names its cause.
      const timer = setTimeout(() => { done(); reject(fail("terminal_input_paused")) }, TYPE_AHEAD_MS)
      const check = (s: CloudTerminalSnapshot) => {
        if (s.canType && this.epoch !== null) { done(); resolve() }
        else if (!s.typeAhead) { done(); reject(this.paused()) }
      }
      this.listeners.add(check)
    })
  }
  /** Why a key cannot be sent or wait now: the state that ended typing, not a generic pause. */
  private paused(): Error {
    const state = this.s.state
    if (state === "revoked" || state === "offline") return fail(this.s.reason || (state === "revoked" ? "terminal_access_revoked" : "machine_offline"))
    if (state === "closed") return fail("terminal_closed")
    if (this.inputUnknown || state === "unknown") return fail("terminal_input_state_unknown")
    if (this.epoch === null || (this.s.control && (!this.s.control.held || !this.s.control.holder?.same_client))) return fail("not_controller")
    if (this.connection && Date.now() >= this.expiresAt) return fail("terminal_stale")
    return fail("terminal_input_paused")
  }
  /** This direct connection's DC can no longer send: fall back to the relay as its close event would. */
  private directClosing(): void {
    if (this.carrier !== "direct") return
    if (this.directAttempt) this.directDown(this.directAttempt, "terminal_direct_closed")
    else void this.fallback()
  }
  private applied(receipt: Receipt, seq: number): void {
    if (receipt.result?.applied_through !== seq) {
      this.inputUnknown = true
      this.set({ state: "unknown", reason: "terminal_input_state_unknown" })
      throw fail("terminal_input_state_unknown")
    }
    // Keys after this one may already be numbered and in flight: the next number only moves forward.
    // Resetting it to seq + 1 here handed a later key the number of one still in flight, which the
    // machine then answered as a duplicate and never typed (a burst lost its fifth key and more).
    this.confirmed = Math.max(this.confirmed, seq)
    this.nextSeq = Math.max(this.nextSeq, seq + 1)
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
      if (event.requestID) {
        const pending = this.pending.get(event.requestID)
        if (pending) {
          clearTimeout(pending.timer)
          this.pending.delete(event.requestID)
          pending.reject(fail(event.error))
          if (["list", "read", "capture", "history"].includes(pending.operation) && event.error === "rate_limited") return
          if (event.requestID === this.renewID && TRANSIENT.has(event.error)) return
        }
      }
      this.inputUnknown = true
      if (event.error === "machine_offline" || event.error === "machine_stale") this.set({ state: "offline", reason: event.error })
      else if (event.error.includes("revoked") || event.error === "terminal_forbidden" || event.error === "forbidden") this.revoke(event.error)
      else this.set({ state: "unknown", reason: event.error })
      return
    }
    const value = event.plaintext as Receipt | Frame | DeltaFrame | Notice
    // A receipt of the connection being replaced still settles its own pending request: an input
    // sent just before a rekey (or before a DC loss) keeps its proof instead of becoming unknown.
    const retiring = value?.type === "terminal_receipt" && !!this.retiringConnection && value.connection === this.retiringConnection
    if (value?.v !== 1 || (value.connection !== this.connection && !retiring)) {
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
      // A settled receipt that is not ok names its status or refusal code, so the timeline keeps it as a failure.
      this.observation?.record("session_settled", { connection: value.connection, requestID: value.request_id,
        operation: value.operation, code: value.status === "ok" ? undefined : value.status === "unknown" ? "unknown" : value.error ?? "refused" })
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
    } else if (value.type === "terminal_frame_delta" && event.envelope.ch.startsWith("termd/")) {
      void this.acceptDelta(value as DeltaFrame, event.envelope)
    } else if (value.type === "terminal_frame") {
      const dropped = !event.envelope.ch.startsWith("term/") || !Number.isSafeInteger(value.frame_seq) || value.frame_seq <= 0 ||
        !completeTerminalFrame(value.frame) ? "terminal_frame_incomplete" :
        typeof value.captured_at !== "number" || Math.abs(Date.now() / 1000 - value.captured_at) > STALE_MS / 1000 ? "terminal_frame_stale" : ""
      if (dropped) { this.observation?.record("frame_dropped", { connection: value.connection, code: dropped }); return }
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
    const dropped = value.connection !== this.connection ? "terminal_old_connection" :
      value.terminal_id !== this.terminal ? "terminal_frame_other_terminal" :
        value.frame_seq <= this.frameSeq ? "terminal_frame_out_of_order" :
          Math.abs(Date.now() / 1000 - value.captured_at) > STALE_MS / 1000 ? "terminal_frame_stale" :
            !completeTerminalFrame(value.frame) ? "terminal_frame_incomplete" : ""
    if (dropped) { this.observation?.record("frame_dropped", { connection: value.connection, code: dropped }); return }
    const first = this.frameSeq === 0
    this.frameSeq = value.frame_seq
    this.frameSeenAt = Date.now()
    this.set({ frame: value.frame, state: value.frame.dead ? "closed" : first ? "just_synced" : "live", reason: "" })
    this.transport.observeTerminalFrame(envelope)
    this.maybeActivate()
    this.maybeUpgrade()
  }
  private async acceptDelta(value: DeltaFrame, envelope: TerminalEnvelope): Promise<void> {
    if (!this.deltaEnabled || !this.active || this.openingNew || !this.terminal || this.deltaChecking) return
    this.deltaChecking = true
    const connection = this.connection
    const terminal = this.terminal
    const baseSeq = this.frameSeq
    const base = this.s.frame
    this.set({ state: "synchronizing" })
    try {
      if (!base || baseSeq === 0) throw fail("terminal_delta_mismatch")
      const frame = await reconstructTerminalDelta(base, baseSeq, value, connection, terminal)
      if (connection !== this.connection || terminal !== this.terminal || baseSeq !== this.frameSeq) return
      this.frameSeq = value.frame_seq
      this.frameSeenAt = Date.now()
      this.set({ frame, state: frame.dead ? "closed" : "live", reason: "" })
      this.transport.observeTerminalFrame(envelope)
    } catch {
      if (connection !== this.connection) return
      this.observation?.record("frame_dropped", { connection, code: "terminal_delta_mismatch" })
      // Nothing more is drawn from this connection; keys typed meanwhile wait (the lease is not lost).
      this.active = false
      this.frameSeq = 0
      this.deltaEnabled = false
      this.closeDirect()
      if (this.carrier === "direct") this.observation?.record("carrier_changed", { connection, code: "relay" })
      this.carrier = "relay"
      this.set({ frame: null, state: "synchronizing", reason: "terminal_delta_mismatch", carrier: "relay" })
      void this.reopen(terminal)
    } finally { this.deltaChecking = false }
  }
  /**
   * Replaces a connection this tab discarded with a new one that reads the terminal again and proves
   * the lease, as after a network loss. The new connection used to be opened and never attached: no
   * screen and no key ever crossed it again.
   */
  private async reopen(terminal: string): Promise<void> {
    try {
      // The machine counts two connections per viewer: the discarded one is given back first, or
      // the new one is refused as busy while it lingers (2026-10-06, 20:41:04). Receipts of keys
      // sent before it still arrive ahead of this one, on the connection still subscribed.
      await this.request("release_connection").catch(() => undefined)
      await this.start({ abandon: true })
      if (terminal && terminal === this.terminal) await this.attach(terminal, false)
    } catch (error) {
      if ((this.s.state as CloudTerminalState) === "revoked") return
      // The discarded connection is not one to rekey from: the next start opens a new one, and the
      // person reconnects from a stale screen, then takes control again.
      if (!this.openingNew && this.connection) { this.transport.unsubscribeTerminal(this.connection); this.connection = "" }
      this.active = false
      this.epoch = null
      this.set({ control: null, state: "stale", reason: (error as Error).message || "terminal_delta_mismatch" })
    }
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
        this.maybeUpgrade()
      })
      .catch((error) => { this.inputUnknown = true; this.set({ state: "unknown", reason: (error as Error).message }) })
      .finally(() => { this.activating = false })
  }
  private checkFreshness(): void {
    if (this.s.state === "revoked") return
    if (this.epoch !== null && this.active && !this.openingNew && !this.retiringConnection &&
      !this.inputUnknown && !this.renewing && Date.now() - this.lastRenew >= RENEW_MS) {
      this.renewing = true
      this.lastRenew = Date.now()
      const epoch = this.epoch
      void this.request("control", { terminal_id: this.terminal, client: this.client, body: { action: "renew" } },
        (id) => { this.renewID = id })
        .then((receipt) => {
          const control = (receipt.result?.control ?? receipt.result) as TerminalControl | undefined
          if (!control?.held || !control.holder?.same_client || !control.holder?.same_device ||
            control.epoch !== this.epoch || (control.expires_at ?? 0) * 1000 <= Date.now() ||
            (control.applied_through ?? 0) < this.confirmed) this.forgetLease("terminal_input_state_unknown")
          else this.set({ control })
        })
        .catch((error: { code?: string; receiptStatus?: string }) => {
          // Unsent, unanswered, or refused for load: the lease's own expiry still bounds typing, and
          // the renewal is asked again soon. Only a machine's answer about the lease ends it.
          if (!error?.receiptStatus || TRANSIENT.has(error.code ?? "")) {
            this.lastRenew = Date.now() - RENEW_MS + RENEW_RETRY_MS
            return
          }
          const now = this.s.state as CloudTerminalState
          if (this.epoch === epoch && now !== "revoked" && now !== "closed") this.forgetLease("terminal_input_state_unknown")
        })
        .finally(() => { this.renewing = false; this.renewID = "" })
    }
    if (this.connection && this.terminal && !this.openingNew && (!this.rotationAttempted || Date.now() >= this.rotationRetryAt) &&
      this.expiresAt - Date.now() < 60_000 && Date.now() < this.expiresAt) {
      this.rotationAttempted = true
      void this.start().catch(() => { this.rotationRetryAt = Date.now() + ROTATION_RETRY_MS })
    }
    if (Date.now() >= this.expiresAt || (this.s.frame && Date.now() - this.frameSeenAt > STALE_MS)) {
      this.set({ state: "stale", reason: "terminal_stale" })
    }
    this.maybeUpgrade()
  }
  dispose(): void {
    this.closeDirect()
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
