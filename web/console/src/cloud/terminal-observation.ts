/** A bounded, content-free browser timeline for one terminal page instance. */
export type TerminalStage = "subscription_confirmed" | "request_pending" | "request_sent" |
  "raw_received" | "envelope_opened" | "envelope_rejected" |
  "pending_match" | "pending_miss" | "session_settled" | "receipt_timeout"
export type TerminalObservationRow = {
  stage: TerminalStage
  connection?: number
  request?: number
  channel?: "term" | "termr"
  operation?: string
  code?: string
}

export class TerminalObservation {
  private readonly connections = new Map<string, number>()
  private readonly requests = new Map<string, number>()
  private count = 0

  constructor(private readonly sink: (row: TerminalObservationRow) => void =
    (row) => console.info("cloud terminal stage", row)) {}

  record(stage: TerminalStage, fields: {
    connection?: string; requestID?: string; channel?: "term" | "termr"; operation?: string; code?: string
  } = {}): void {
    if (this.count >= 128) return
    this.count++
    const row: TerminalObservationRow = { stage }
    if (fields.connection) row.connection = this.ordinal(this.connections, fields.connection)
    if (fields.requestID) row.request = this.ordinal(this.requests, fields.requestID)
    if (fields.channel) row.channel = fields.channel
    if (fields.operation) row.operation = /^[a-z_]{1,32}$/.test(fields.operation) ? fields.operation : "unrecognized"
    if (fields.code) row.code = /^[a-z_]{1,48}$/.test(fields.code) ? fields.code : "terminal_receive_failed"
    try { this.sink(row) } catch { /* Diagnostics cannot affect terminal delivery. */ }
  }

  private ordinal(map: Map<string, number>, id: string): number {
    const found = map.get(id)
    if (found) return found
    const next = map.size + 1
    map.set(id, next)
    return next
  }
}
