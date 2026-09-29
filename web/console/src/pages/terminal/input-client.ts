import type { TerminalControl, TerminalHolder, TerminalRefusalCode } from "@clawdline/contract"

/**
 * Everything one browser tab types into one terminal (plan v3 D3, D4), with
 * no DOM in it so each rule below can be driven by a test.
 *
 * The lease: `acquire`/`takeover` make a lease this tab holds, a renew every
 * 10 seconds keeps it, `release` gives it back (and `releaseOnLeave` does the
 * same from `pagehide` with a keepalive request, which outlives the tab).
 *
 * The keystrokes: bytes are gathered for 8 ms, or for as long as a request is
 * in flight, and sent in batches of at most 4 KiB, each numbered 1, 2, 3 …
 * within the lease. A paste is never mixed into a batch; it goes to `/paste`
 * in its turn. At most 64 KiB wait here; past that no key is taken and the
 * page says the input is full.
 *
 * The numbering rules, which is where a keystroke can be typed twice or lost:
 *
 * - Within one lease a batch is only ever sent again under its own number: a
 *   network error or `terminal_busy` resends the same batch with the same
 *   seq, so the daemon types it at most once (a repeat answers `duplicate`).
 * - Whenever the lease changes, for any reason — `input_state_unknown`,
 *   `lease_expired`, `lease_superseded`, `not_controller`, a `control` event
 *   naming another holder or epoch, or any new acquire (after a network error
 *   or a daemon restart the new lease's number can even equal the old one) —
 *   every byte the daemon has not confirmed with `applied_through` is dropped,
 *   and the person is told to look at the screen before typing again. A
 *   keystroke typed into a shell whose state is unknown must never be
 *   replayed blind.
 */

/** Keystrokes in one request: `terminal.input_bytes`. */
export const MAX_BATCH_BYTES = 4 * 1024
/** Keystrokes waiting in this tab: `terminal.client_queue_bytes`. */
export const MAX_QUEUED_BYTES = 64 * 1024
/** How long keystrokes gather before a request is sent. */
export const BATCH_MS = 8
/** A lease lasts 30 s on the daemon and is renewed every 10. */
export const RENEW_MS = 10_000
/** No beat for longer than this and the screen may be stale (the daemon beats every 5 s). */
export const STALE_MS = 6_000
/** The first wait before the same batch is sent again. */
export const RETRY_MS = 60
/** The longest wait between resends of the same batch. */
export const RETRY_MAX_MS = 2_000

/** What a request answered: its HTTP status and its decoded body. */
export interface Answer {
  status: number
  body: unknown
}

/** How requests leave this tab. It throws when no answer came at all. */
export interface Transport {
  post(path: string, body: unknown, opts?: { keepalive?: boolean }): Promise<Answer>
}

export interface Clock {
  now(): number
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
}

export const realClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
  clearTimeout: (h) => globalThis.clearTimeout(h as ReturnType<typeof setTimeout>),
}

/** Why the bytes not yet confirmed were dropped, or why typing stopped. */
export type InputNotice =
  | TerminalRefusalCode
  | "network"
  | "reacquired"
  | "released"

export interface InputState {
  /** This tab holds the lease now. */
  holding: boolean
  epoch: number | null
  /** The highest input number the daemon confirmed under `epoch`. */
  appliedThrough: number
  /** Bytes waiting, in flight included. */
  queued: number
  /** Past 64 KiB: keys are refused until the queue drains. */
  full: boolean
  /** No beat for 6 s: typing is off until the stream is heard again. */
  stale: boolean
  /** A batch is waiting for its answer, or waiting to be resent. */
  sending: boolean
  /** The last request got no answer; the same batch is being resent. */
  retrying: boolean
  /** Why unconfirmed bytes were dropped or typing stopped; cleared on the next acquire. */
  notice: InputNotice | null
  /** How many bytes that notice dropped. */
  dropped: number
  /** Who holds the lease when an acquire was refused `terminal_controlled`. */
  holder: TerminalHolder | null
  /** A control request is on its way. */
  controlling: boolean
}

type Item = { kind: "bytes"; data: Uint8Array } | { kind: "paste"; text: string }
interface Flight {
  seq: number
  item: Item
  epoch: number
}

const encoder = new TextEncoder()

function size(item: Item): number {
  return item.kind === "bytes" ? item.data.length : encoder.encode(item.text).length
}

function base64(data: Uint8Array): string {
  let s = ""
  for (let i = 0; i < data.length; i++) s += String.fromCharCode(data[i])
  return btoa(s)
}

function refusalOf(body: unknown): { error: TerminalRefusalCode | ""; holder?: TerminalHolder; applied_through?: number } {
  const b = body as { error?: unknown; holder?: TerminalHolder; applied_through?: unknown } | null
  return {
    error: typeof b?.error === "string" ? (b.error as TerminalRefusalCode) : "",
    holder: b?.holder,
    applied_through: typeof b?.applied_through === "number" ? b.applied_through : undefined,
  }
}

/** Codes after which this lease can take no more input. */
const LEASE_ENDING = new Set<string>([
  "lease_superseded", "lease_expired", "input_state_unknown", "not_controller", "input_gap",
  "terminal_closed", "terminal_forbidden", "terminal_access_revoked", "terminal_unreachable",
  "terminal_unsupported", "terminal_cloud_not_supported", "terminal_invalid", "input_too_large",
])

export class TerminalInputClient {
  private readonly base: string
  private queue: Item[] = []
  private flight: Flight | null = null
  private nextSeq = 1
  private batchTimer: unknown = null
  private retryTimer: unknown = null
  private retryDelay = RETRY_MS
  private renewTimer: unknown = null
  private staleTimer: unknown = null
  private heard: number
  private listeners = new Set<(s: InputState) => void>()
  private s: InputState = {
    holding: false, epoch: null, appliedThrough: 0, queued: 0, full: false, stale: false, sending: false,
    retrying: false, notice: null, dropped: 0, holder: null, controlling: false,
  }

  readonly terminal: string
  readonly client: string
  private readonly transport: Transport
  private readonly clock: Clock

  constructor(terminal: string, client: string, transport: Transport, clock: Clock = realClock) {
    this.terminal = terminal
    this.client = client
    this.transport = transport
    this.clock = clock
    this.base = "/v1/terminals/" + encodeURIComponent(terminal)
    this.heard = clock.now()
    this.watchStale()
  }

  get state(): InputState {
    return this.s
  }

  subscribe(fn: (s: InputState) => void): () => void {
    this.listeners.add(fn)
    fn(this.s)
    return () => this.listeners.delete(fn)
  }

  /** Keys are taken now: the lease is held, the screen is fresh and the queue has room. */
  get canType(): boolean {
    return this.s.holding && !this.s.stale && !this.s.full
  }

  // ---- the lease

  /** `acquire`, or `takeover` of somebody else's lease. Resolves with the refusal code, or "" when held. */
  async control(action: "acquire" | "takeover"): Promise<TerminalRefusalCode | "network" | ""> {
    this.set({ controlling: true })
    let answer: Answer
    try {
      answer = await this.transport.post(this.base + "/control", { action, client: this.client })
    } catch {
      this.set({ controlling: false })
      return "network"
    }
    if (answer.status !== 200) {
      const r = refusalOf(answer.body)
      this.set({ controlling: false, holder: r.holder ?? null })
      return r.error || "terminal_unreachable"
    }
    const c = answer.body as TerminalControl
    // A new lease, whatever its number: nothing typed under an earlier one is
    // resent into it (D4 client rule).
    // A notice from the lease that ended stays up: the person still has to
    // look at the screen before typing, and the first key they type clears it.
    this.dropUnconfirmed(this.s.holding || this.pending() > 0 ? "reacquired" : null)
    this.nextSeq = (c.applied_through ?? 0) + 1
    this.set({
      holding: true, epoch: c.epoch, appliedThrough: c.applied_through ?? 0, controlling: false, holder: null,
      notice: this.s.notice === "released" ? null : this.s.notice,
    })
    this.scheduleRenew()
    this.flushSoon()
    return ""
  }

  async release(): Promise<void> {
    const epoch = this.s.epoch
    this.endLease("released")
    if (epoch === null) return
    try {
      await this.transport.post(this.base + "/control", { action: "release", client: this.client })
    } catch {
      // refusal-ok: an unanswered release lapses on its own in 30 s.
    }
  }

  /** From `pagehide`: the request outlives the tab. */
  releaseOnLeave(): void {
    if (!this.s.holding) return
    void this.transport.post(this.base + "/control", { action: "release", client: this.client }, { keepalive: true })
      .catch(() => undefined)
  }

  private scheduleRenew(): void {
    if (this.renewTimer !== null) this.clock.clearTimeout(this.renewTimer)
    this.renewTimer = this.clock.setTimeout(() => void this.renew(), RENEW_MS)
  }

  private async renew(): Promise<void> {
    this.renewTimer = null
    if (!this.s.holding) return
    const epoch = this.s.epoch
    let answer: Answer
    try {
      answer = await this.transport.post(this.base + "/control", { action: "renew", client: this.client })
    } catch {
      // The lease has 20 s left; the next renew may be heard.
      if (this.s.holding && this.s.epoch === epoch) this.scheduleRenew()
      return
    }
    if (!this.s.holding || this.s.epoch !== epoch) return
    if (answer.status !== 200) {
      this.endLease(refusalOf(answer.body).error || "terminal_unreachable")
      return
    }
    const c = answer.body as TerminalControl
    if (!c.held || c.epoch !== epoch || !c.holder?.same_client) {
      this.endLease("lease_superseded")
      return
    }
    this.scheduleRenew()
  }

  /** The stream's `control` event: another holder or another epoch ends this tab's lease. */
  controlEvent(c: TerminalControl): void {
    if (!this.s.holding) return
    if (c.held && c.holder?.same_client && c.epoch === this.s.epoch) return
    this.endLease(c.held ? "lease_superseded" : "lease_expired")
  }

  /** The stream said the terminal ended or was closed: nothing more can be typed. */
  terminalGone(code: "terminal_closed" | "terminal_access_revoked"): void {
    this.endLease(code)
  }

  private endLease(notice: InputNotice): void {
    const hadBytes = this.pending() > 0
    this.dropUnconfirmed(notice)
    if (this.renewTimer !== null) this.clock.clearTimeout(this.renewTimer)
    this.renewTimer = null
    this.set({ holding: false, epoch: null, notice: notice === "released" && !hadBytes ? null : notice })
  }

  // ---- freshness

  /** A `beat` or a `frame` arrived: the stream is alive. */
  heardStream(): void {
    this.heard = this.clock.now()
    if (this.s.stale) {
      this.set({ stale: false })
      this.flushSoon()
    }
  }

  private watchStale(): void {
    this.staleTimer = this.clock.setTimeout(() => {
      if (!this.s.stale && this.clock.now() - this.heard > STALE_MS) this.set({ stale: true })
      this.watchStale()
    }, 1_000)
  }

  // ---- typing

  private pending(): number {
    return this.queue.reduce((n, item) => n + size(item), 0) + (this.flight ? size(this.flight.item) : 0)
  }

  /** Keystroke bytes from the terminal. False when they were not taken. */
  type(data: Uint8Array): boolean {
    if (!data.length) return true
    if (!this.s.holding || this.s.stale) return false
    if (this.pending() + data.length > MAX_QUEUED_BYTES) {
      this.set({ full: true })
      return false
    }
    const last = this.queue[this.queue.length - 1]
    if (last?.kind === "bytes" && last.data.length + data.length <= MAX_BATCH_BYTES) {
      const joined = new Uint8Array(last.data.length + data.length)
      joined.set(last.data)
      joined.set(data, last.data.length)
      this.queue[this.queue.length - 1] = { kind: "bytes", data: joined }
    } else {
      for (let at = 0; at < data.length; at += MAX_BATCH_BYTES) {
        this.queue.push({ kind: "bytes", data: data.slice(at, at + MAX_BATCH_BYTES) })
      }
    }
    this.set({ queued: this.pending(), notice: null, dropped: 0 })
    this.flushSoon()
    return true
  }

  /** A paste: always to `/paste`, in its turn among the keystrokes. */
  paste(text: string): boolean {
    if (!text) return true
    if (!this.s.holding || this.s.stale) return false
    this.queue.push({ kind: "paste", text })
    this.set({ queued: this.pending() })
    this.flushSoon()
    return true
  }

  /** Stop every timer. */
  dispose(): void {
    for (const t of [this.batchTimer, this.retryTimer, this.renewTimer, this.staleTimer]) {
      if (t !== null) this.clock.clearTimeout(t)
    }
    this.batchTimer = this.retryTimer = this.renewTimer = this.staleTimer = null
    this.listeners.clear()
  }

  private flushSoon(): void {
    if (this.batchTimer !== null || this.flight || !this.queue.length) return
    this.batchTimer = this.clock.setTimeout(() => {
      this.batchTimer = null
      this.sendNext()
    }, BATCH_MS)
  }

  private sendNext(): void {
    if (this.flight || !this.queue.length || !this.s.holding || this.s.epoch === null || this.s.stale) return
    const item = this.queue.shift()!
    this.flight = { seq: this.nextSeq++, item, epoch: this.s.epoch }
    this.retryDelay = RETRY_MS
    this.set({ sending: true })
    void this.sendFlight()
  }

  private async sendFlight(): Promise<void> {
    const f = this.flight
    if (!f) return
    const path = this.base + (f.item.kind === "paste" ? "/paste" : "/input")
    const body = f.item.kind === "paste"
      ? { epoch: f.epoch, client: this.client, seq: f.seq, text: f.item.text }
      : { epoch: f.epoch, client: this.client, seq: f.seq, data: base64(f.item.data) }
    let answer: Answer
    try {
      answer = await this.transport.post(path, body)
    } catch {
      if (this.flight !== f) return
      this.set({ retrying: true })
      this.retryLater(f)
      return
    }
    if (this.flight !== f) return
    if (answer.status === 200) {
      const applied = (answer.body as { applied_through?: number }).applied_through ?? f.seq
      this.flight = null
      this.set({ appliedThrough: Math.max(this.s.appliedThrough, applied), sending: false, retrying: false,
        queued: this.pending(), full: false })
      // Whatever gathered while this was in flight goes now, as one batch.
      this.sendNext()
      return
    }
    const r = refusalOf(answer.body)
    if (r.error === "terminal_busy" || (answer.status >= 500 && !r.error)) {
      this.set({ retrying: r.error !== "terminal_busy" })
      this.retryLater(f)
      return
    }
    if (r.error === "input_gap" && r.applied_through !== undefined && r.applied_through >= f.seq) {
      // It was typed; the answer that said so was lost.
      this.flight = null
      this.set({ appliedThrough: r.applied_through, sending: false, retrying: false, queued: this.pending() })
      this.sendNext()
      return
    }
    this.endLease(r.error && LEASE_ENDING.has(r.error) ? r.error : (r.error || "terminal_unreachable"))
  }

  /** The same batch, the same seq, after a growing wait. */
  private retryLater(f: Flight): void {
    const delay = this.retryDelay
    this.retryDelay = Math.min(RETRY_MAX_MS, this.retryDelay * 2)
    this.retryTimer = this.clock.setTimeout(() => {
      this.retryTimer = null
      if (this.flight === f) void this.sendFlight()
    }, delay)
  }

  private dropUnconfirmed(notice: InputNotice | null): void {
    const dropped = this.pending()
    this.queue = []
    this.flight = null
    if (this.batchTimer !== null) this.clock.clearTimeout(this.batchTimer)
    if (this.retryTimer !== null) this.clock.clearTimeout(this.retryTimer)
    this.batchTimer = this.retryTimer = null
    this.set({ queued: 0, full: false, sending: false, retrying: false,
      ...(notice ? { notice, dropped } : {}) })
  }

  private set(patch: Partial<InputState>): void {
    this.s = { ...this.s, ...patch }
    for (const fn of this.listeners) fn(this.s)
  }
}
