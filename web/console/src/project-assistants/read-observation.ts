export type BotReadStage = "started" | "admitted" | "sealed" | "sent" | "relay_ack" | "relay_refused" | "response_frame" | "response_open_started" |
  "response_open" | "validated" | "ready" | "committed" | "paint_opportunity"
export type BotReadMode = "snapshot" | "history"
type Outcome = "paint_opportunity" | "committed" | "refused" | "abandoned"

export class BotReadObservation {
  readonly id = "bot-view-" + crypto.randomUUID()
  private readonly stages: Partial<Record<`${BotReadStage}_ms`, number>> = {}
  private sequence?: number
  private responseSequence?: number
  private responseCiphertextBytes?: number
  private finished = false

  private readonly mode: BotReadMode
  private readonly start: number
  private readonly interactionID: string
  private readonly report: (data: Record<string, unknown>) => void

  constructor(mode: BotReadMode, start: number, report: (data: Record<string, unknown>) => void, interactionID?: string) {
    this.mode = mode
    this.start = start
    this.interactionID = interactionID ?? this.id
    this.report = report
    this.mark("started")
  }

  mark = (stage: BotReadStage, at = performance.now(), sequence?: number, ciphertextBytes?: number) => {
    if (this.finished) return
    this.stages[`${stage}_ms`] = Math.max(0, Math.round(at - this.start))
    if (stage === "sealed") {
      this.sequence = sequence
      delete this.stages.sent_ms
      delete this.stages.relay_ack_ms
      delete this.stages.relay_refused_ms
    }
    if (stage === "response_frame") {
      this.responseSequence = sequence
      this.responseCiphertextBytes = ciphertextBytes
    }
  }

  finish(outcome: Outcome, code?: string) {
    if (this.finished) return
    this.finished = true
    this.report({
      trace_id: this.id, interaction_id: this.interactionID, mode: this.mode, outcome, sequence: this.sequence, response_sequence: this.responseSequence,
      total_ms: Math.max(0, Math.round(performance.now() - this.start)),
      ...(this.responseCiphertextBytes === undefined ? {} : { response_ciphertext_base64_bytes: this.responseCiphertextBytes }),
      ...this.stages, ...(code ? { code } : {}),
    })
  }
}
