/** A bounded, content-free browser timeline for one terminal page instance. */
export type TerminalStage = "subscription_sent" | "subscription_confirmed" | "subscription_refused" | "relay_error" | "request_pending" | "request_sent" | "relay_ack" | "publish_refused" |
  "raw_received" | "envelope_opened" | "envelope_rejected" |
  "pending_match" | "pending_miss" | "session_settled" | "receipt_timeout" | "frame_observed" |
  /** The direct (WebRTC) carrier: `code` names the carrier a connection now uses, or why an upgrade or DC ended. */
  "carrier_changed" | "direct_failed" | "direct_down"
export type TerminalObservationRow = {
  /** This page's own count, 1 to 128. */
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

export const TERMINAL_OBSERVATION_ROWS = 128

const phases: Record<TerminalStage, TerminalPhase | null> = {
  subscription_sent: null, subscription_confirmed: null, subscription_refused: null, relay_error: null,
  request_pending: null, request_sent: null, relay_ack: null, publish_refused: "relay_publish",
  raw_received: "ciphertext", envelope_opened: "verify_decrypt", envelope_rejected: "verify_decrypt",
  pending_match: "pending_match", pending_miss: "pending_match", session_settled: "session_accept",
  receipt_timeout: "receipt_timeout", frame_observed: null,
  carrier_changed: null, direct_failed: null, direct_down: null,
}
const failures = new Set<TerminalStage>(["publish_refused", "envelope_rejected", "pending_miss", "receipt_timeout"])

/** One row as a single line with its fields always in the same order, so a console that shows objects as `Object` still shows every field. */
export function terminalStageLine(row: TerminalObservationRow): string {
  return `cloud terminal stage seq=${row.seq} t=+${row.ms}ms stage=${row.stage} phase=${phases[row.stage] ?? "-"}` +
    ` conn=${row.connection ?? "-"} req=${row.request ?? "-"} ch=${row.channel ?? "-"} op=${row.operation ?? "-"} code=${row.code ?? "-"}`
}

export class TerminalObservation {
  private readonly connections = new Map<string, number>()
  private readonly requests = new Map<string, number>()
  private readonly sink: ((row: TerminalObservationRow) => void) | null
  private readonly rows: TerminalObservationRow[] = []
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
    if (!this.sink || this.rows.length >= TERMINAL_OBSERVATION_ROWS) return
    try {
      const row: TerminalObservationRow = { seq: this.rows.length + 1, ms: Math.max(0, Math.round(this.clock() - this.began)), stage }
      if (fields.connection) row.connection = this.ordinal(this.connections, fields.connection)
      if (fields.requestID) row.request = this.ordinal(this.requests, fields.requestID)
      if (fields.channel) row.channel = fields.channel
      if (fields.operation) row.operation = /^[a-z_]{1,32}$/.test(fields.operation) ? fields.operation : "unrecognized"
      if (fields.code) row.code = /^[a-z_]{1,48}$/.test(fields.code) ? fields.code : "terminal_receive_failed"
      this.rows.push(row)
      this.sink(row)
    } catch { /* Diagnostics cannot affect terminal delivery. */ }
  }

  /** The first stage at which a receipt failed, or null while nothing has. */
  stopped(): { phase: TerminalPhase; row: TerminalObservationRow } | null {
    const row = this.rows.find((r) => failures.has(r.stage))
    return row ? { phase: phases[row.stage]!, row } : null
  }

  /** Durations from this page's monotonic clock; absent means the stage was not observed. */
  timings(): { subscriptionMs?: number; openReceiptMs?: number; firstFrameMs?: number; listReceiptMs?: number; inputReceiptMaxMs?: number } {
    const result: { subscriptionMs?: number; openReceiptMs?: number; firstFrameMs?: number; listReceiptMs?: number; inputReceiptMaxMs?: number } = {}
    result.subscriptionMs = this.rows.find((row) => row.stage === "subscription_confirmed")?.ms
    result.firstFrameMs = this.rows.find((row) => row.stage === "frame_observed")?.ms
    const sent = new Map<number, TerminalObservationRow>()
    for (const row of this.rows) {
      if (row.stage === "request_sent" && row.request) sent.set(row.request, row)
      if (row.stage !== "session_settled" || !row.request) continue
      const start = sent.get(row.request)
      if (!start) continue
      const elapsed = row.ms - start.ms
      if (row.operation === "open_connection" && result.openReceiptMs === undefined) result.openReceiptMs = elapsed
      if (row.operation === "list" && result.listReceiptMs === undefined) result.listReceiptMs = elapsed
      if ((row.operation === "input" || row.operation === "paste") &&
        (result.inputReceiptMaxMs === undefined || elapsed > result.inputReceiptMaxMs)) result.inputReceiptMaxMs = elapsed
    }
    return result
  }

  /** The whole timeline as text a person can copy: fixed fields, no terminal contents, keys or real identifiers. */
  text(): string {
    const stopped = this.stopped()
    const timing = this.timings()
    const head = [`cloud terminal diagnostics started=${this.startedAt} rows=${this.rows.length}/${TERMINAL_OBSERVATION_ROWS}`,
      stopped ? `stopped phase=${stopped.phase} stage=${stopped.row.stage} code=${stopped.row.code ?? "-"} seq=${stopped.row.seq}` : "stopped phase=-"]
    if (Object.values(timing).some((value) => value !== undefined)) head.push(
      `timings subscription=${timing.subscriptionMs ?? "-"}ms open_receipt=${timing.openReceiptMs ?? "-"}ms` +
      ` first_frame=${timing.firstFrameMs ?? "-"}ms list_receipt=${timing.listReceiptMs ?? "-"}ms` +
      ` input_receipt_max=${timing.inputReceiptMaxMs ?? "-"}ms`)
    return [...head, ...this.rows.map(terminalStageLine)].join("\n")
  }

  private ordinal(map: Map<string, number>, id: string): number {
    const found = map.get(id)
    if (found) return found
    const next = map.size + 1
    map.set(id, next)
    return next
  }
}
