import { hostedConsole } from "../pages/terminal/api"
import { decisionTraceBody, type DecisionTraceStep } from "./decision-trace"

/** Fire and forget; the hosted console has no daemon of its own to write to. */
export function traceDecision(step: DecisionTraceStep): void {
  if (hostedConsole()) return
  try {
    void fetch("/v1/project-assistants/decision-trace", {
      method: "POST", cache: "no-store", keepalive: true,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(decisionTraceBody(step)),
    }).catch(() => {})
  } catch { /* refusal-ok: tracing never affects the card */ }
}
