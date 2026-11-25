/**
 * One step of a decision card's send path, written to this machine's daemon
 * log (internal/transport/http/decision_trace.go). A card that kept spinning
 * after its answer had reached the chat left no evidence of which step
 * stalled; these steps, joined to the daemon's own "bots answer" lines by the
 * idempotency key, show it. Identifiers, step names, codes and durations
 * only: never the reply text.
 */
export type DecisionTracePhase = "press" | "request" | "settled" | "failed" | "check" | "unmounted" | "timeout"
export type DecisionTraceStep = {
  attempt: string
  phase: DecisionTracePhase
  bot_id: string
  offer_id: string
  option?: string
  idempotency_key?: string
  code?: string
  ms?: number
}

/** A press still unsettled after this long is logged once as "timeout"; nothing else changes. */
export const DECISION_TRACE_SLOW_MS = 15000

const token = (value: string | undefined) => (value ?? "").replace(/[^A-Za-z0-9._:-]/g, "_")

export function newDecisionAttempt(): string {
  const bytes = new Uint8Array(6)
  crypto.getRandomValues(bytes)
  return "dt-" + Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("")
}

export function decisionTraceBody(step: DecisionTraceStep) {
  return {
    attempt: token(step.attempt), phase: step.phase, bot_id: token(step.bot_id), offer_id: token(step.offer_id),
    option: token(step.option), idempotency_key: token(step.idempotency_key), code: token(step.code),
    ms: Math.max(0, Math.round(step.ms ?? 0)),
  }
}
