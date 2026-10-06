/** A bounded, content-free browser timeline for one terminal page instance. */
export type TerminalStage = "subscription_sent" | "subscription_confirmed" | "subscription_refused" | "relay_error" | "request_pending" | "request_sent" | "relay_ack" | "publish_refused" |
  /** The relay refused a publish while busy; the same request goes again shortly. */
  "publish_retry" |
  "raw_received" | "envelope_opened" | "envelope_rejected" |
  "pending_match" | "pending_miss" | "session_settled" | "receipt_timeout" | "frame_observed" |
  /** A verified frame the session set aside without drawing; `code` says why. */
  "frame_dropped" |
  /** The direct (WebRTC) carrier: `code` names the carrier a connection now uses, or why an upgrade or DC ended. */
  "carrier_changed" | "direct_failed" | "direct_down"
export type TerminalObservationRow = {
  /** This page's own count of every stage it recorded, from 1; it keeps counting when older rows are evicted. */
  seq: number
  /** Milliseconds since this page instance began observing. */
  ms: number
  stage: TerminalStage
  connection?: number
  request?: number
  channel?: "term" | "termr"
  operation?: string
  code?: string
}
/** Where a receipt stopped, in the order a receipt passes through the browser. */
export type TerminalPhase = "relay_publish" | "ciphertext" | "verify_decrypt" | "pending_match" | "session_accept" | "receipt_timeout"

/**
 * How a page keeps its timeline bounded without going silent (docs/cloud-terminal-wire.md, Diagnostics):
 * the report keeps the newest TERMINAL_OBSERVATION_ROWS rows, evicting the oldest, and pins the first
 * TERMINAL_OBSERVATION_PINNED notable rows (failures, refusals, timeouts, drops, carrier changes) so it
 * shows both how a terminal started going wrong and its latest state. Until 2026-10 the page kept the
 * first 128 rows and dropped everything after, so a failure after a few dozen keys was never recorded.
 * The console writes the first TERMINAL_OBSERVATION_ROWS rows of each kind, then every
 * TERMINAL_OBSERVATION_ROUTINE_EVERY-th routine row and every TERMINAL_OBSERVATION_NOTABLE_EVERY-th
 * notable row; `seq` counts every row, so a gap in it shows the sampling.
 */
export const TERMINAL_OBSERVATION_ROWS = 128
export const TERMINAL_OBSERVATION_PINNED = 16
export const TERMINAL_OBSERVATION_ROUTINE_EVERY = 64
export const TERMINAL_OBSERVATION_NOTABLE_EVERY = 16
/** Real identifiers remembered for ordinals and receipt timing; the oldest is forgotten first. */
const TERMINAL_OBSERVATION_IDS = 256

const phases: Record<TerminalStage, TerminalPhase | null> = {
  subscription_sent: null, subscription_confirmed: null, subscription_refused: null, relay_error: null,
  request_pending: null, request_sent: null, relay_ack: null, publish_refused: "relay_publish", publish_retry: "relay_publish",
  raw_received: "ciphertext", envelope_opened: "verify_decrypt", envelope_rejected: "verify_decrypt",
  pending_match: "pending_match", pending_miss: "pending_match", session_settled: "session_accept",
  receipt_timeout: "receipt_timeout", frame_observed: null, frame_dropped: "session_accept",
  carrier_changed: null, direct_failed: null, direct_down: null,
}
const failures = new Set<TerminalStage>(["publish_refused", "envelope_rejected", "pending_miss", "receipt_timeout", "frame_dropped"])
/** Stages worth keeping after the rows around them are evicted; `session_settled` is notable when it carries a code (refused or unknown). */
const notableStages = new Set<TerminalStage>([...failures, "subscription_refused", "relay_error", "publish_retry",
  "carrier_changed", "direct_failed", "direct_down"])
function notable(row: TerminalObservationRow): boolean {
  return notableStages.has(row.stage) || (row.stage === "session_settled" && row.code !== undefined)
}
type Timings = { subscriptionMs?: number; openReceiptMs?: number; firstFrameMs?: number; listReceiptMs?: number; inputReceiptMaxMs?: number }

/** One row as a single line with its fields always in the same order, so a console that shows objects as `Object` still shows every field. */
export function terminalStageLine(row: TerminalObservationRow): string {
  return `cloud terminal stage seq=${row.seq} t=+${row.ms}ms stage=${row.stage} phase=${phases[row.stage] ?? "-"}` +
    ` conn=${row.connection ?? "-"} req=${row.request ?? "-"} ch=${row.channel ?? "-"} op=${row.operation ?? "-"} code=${row.code ?? "-"}`
}

export class TerminalObservation {
  private readonly connections = new Map<string, number>()
  private readonly requests = new Map<string, number>()
  private readonly sink: ((row: TerminalObservationRow) => void) | null
  /** The newest rows, oldest first. */
  private readonly rows: TerminalObservationRow[] = []
  /** The first notable rows, kept after the ring evicts them. */
  private readonly pinned: TerminalObservationRow[] = []
  private firstFailure: TerminalObservationRow | null = null
  private lastNotable: TerminalObservationRow | null = null
  private seen = 0
  private routineSeen = 0
  private notableSeen = 0
  private connectionCount = 0
  private requestCount = 0
  /** Timings accumulate as rows arrive, so they survive the rows' eviction. */
  private readonly timing: Timings = {}
  private readonly sent = new Map<number, number>()
  private readonly began: number
  private readonly startedAt: string
  private readonly clock: () => number

  /** A `null` sink turns the timeline off: nothing is kept and nothing is written. */
  constructor(sink: ((row: TerminalObservationRow) => void) | null =
    (row) => console.info(terminalStageLine(row)), clock: () => number = () => performance.now()) {
    this.sink = sink
    this.clock = clock
    this.began = clock()
    this.startedAt = new Date().toISOString()
  }

  record(stage: TerminalStage, fields: {
    connection?: string; requestID?: string; channel?: "term" | "termr"; operation?: string; code?: string
  } = {}): void {
    if (!this.sink) return
    try {
      const row: TerminalObservationRow = { seq: this.seen + 1, ms: Math.max(0, Math.round(this.clock() - this.began)), stage }
      if (fields.connection) row.connection = this.ordinal(this.connections, fields.connection, () => ++this.connectionCount)
      if (fields.requestID) row.request = this.ordinal(this.requests, fields.requestID, () => ++this.requestCount)
      if (fields.channel) row.channel = fields.channel
      if (fields.operation) row.operation = /^[a-z_]{1,32}$/.test(fields.operation) ? fields.operation : "unrecognized"
      if (fields.code) row.code = /^[a-z_]{1,48}$/.test(fields.code) ? fields.code : "terminal_receive_failed"
      this.seen = row.seq
      this.rows.push(row)
      if (this.rows.length > TERMINAL_OBSERVATION_ROWS) this.rows.shift()
      this.time(row)
      let write: boolean
      if (notable(row)) {
        this.notableSeen++
        this.lastNotable = row
        if (!this.firstFailure && failures.has(row.stage)) this.firstFailure = row
        if (this.pinned.length < TERMINAL_OBSERVATION_PINNED) this.pinned.push(row)
        write = this.notableSeen <= TERMINAL_OBSERVATION_ROWS || this.notableSeen % TERMINAL_OBSERVATION_NOTABLE_EVERY === 0
      } else {
        this.routineSeen++
        write = this.routineSeen <= TERMINAL_OBSERVATION_ROWS || this.routineSeen % TERMINAL_OBSERVATION_ROUTINE_EVERY === 0
      }
      if (write) this.sink(row)
    } catch { /* Diagnostics cannot affect terminal delivery. */ }
  }

  /** The first stage at which a receipt failed, or null while nothing has. */
  stopped(): { phase: TerminalPhase; row: TerminalObservationRow } | null {
    const row = this.firstFailure
    return row ? { phase: phases[row.stage]!, row } : null
  }

  /** Durations from this page's monotonic clock; absent means the stage was not observed. */
  timings(): Timings {
    return { ...this.timing }
  }

  /** The whole timeline as text a person can copy: fixed fields, no terminal contents, keys or real identifiers. */
  text(): string {
    const stopped = this.stopped()
    const timing = this.timings()
    const kept = new Set(this.rows.map((row) => row.seq))
    const early = this.pinned.filter((row) => !kept.has(row.seq))
    const omitted = this.seen - this.rows.length - early.length
    const head = [`cloud terminal diagnostics started=${this.startedAt} rows=${this.rows.length}/${TERMINAL_OBSERVATION_ROWS}` +
      (omitted > 0 ? ` omitted=${omitted}` : ""),
      stopped ? `stopped phase=${stopped.phase} stage=${stopped.row.stage} code=${stopped.row.code ?? "-"} seq=${stopped.row.seq}` : "stopped phase=-"]
    const last = this.lastNotable
    if (last && last !== stopped?.row) head.push(
      `latest_notable phase=${phases[last.stage] ?? "-"} stage=${last.stage} code=${last.code ?? "-"} seq=${last.seq}`)
    if (Object.values(timing).some((value) => value !== undefined)) head.push(
      `timings subscription=${timing.subscriptionMs ?? "-"}ms open_receipt=${timing.openReceiptMs ?? "-"}ms` +
      ` first_frame=${timing.firstFrameMs ?? "-"}ms list_receipt=${timing.listReceiptMs ?? "-"}ms` +
      ` input_receipt_max=${timing.inputReceiptMaxMs ?? "-"}ms`)
    const body = early.length ? [...early.map(terminalStageLine), `omitted ${omitted} rows; newest ${this.rows.length} follow`] :
      omitted > 0 ? [`omitted ${omitted} rows; newest ${this.rows.length} follow`] : []
    return [...head, ...body, ...this.rows.map(terminalStageLine)].join("\n")
  }

  private time(row: TerminalObservationRow): void {
    const t = this.timing
    if (row.stage === "subscription_confirmed" && t.subscriptionMs === undefined) t.subscriptionMs = row.ms
    if (row.stage === "frame_observed" && t.firstFrameMs === undefined) t.firstFrameMs = row.ms
    if (row.stage === "request_sent" && row.request) {
      this.sent.delete(row.request)
      this.sent.set(row.request, row.ms)
      if (this.sent.size > TERMINAL_OBSERVATION_IDS) this.sent.delete(this.sent.keys().next().value!)
    }
    if (row.stage !== "session_settled" || !row.request) return
    const start = this.sent.get(row.request)
    if (start === undefined) return
    const elapsed = row.ms - start
    if (row.operation === "open_connection" && t.openReceiptMs === undefined) t.openReceiptMs = elapsed
    if (row.operation === "list" && t.listReceiptMs === undefined) t.listReceiptMs = elapsed
    if ((row.operation === "input" || row.operation === "paste") &&
      (t.inputReceiptMaxMs === undefined || elapsed > t.inputReceiptMaxMs)) t.inputReceiptMaxMs = elapsed
  }

  /** A small number standing for a real identifier; numbers are never reused, and the oldest identifiers are forgotten. */
  private ordinal(map: Map<string, number>, id: string, next: () => number): number {
    const found = map.get(id)
    if (found) return found
    const assigned = next()
    map.set(id, assigned)
    if (map.size > TERMINAL_OBSERVATION_IDS) map.delete(map.keys().next().value!)
    return assigned
  }
}
