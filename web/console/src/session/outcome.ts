/*
 * What a write that failed may have done (F3).
 *
 * Two answers, and the burden is on the certain one. `not_done` is a refusal
 * this daemon gives only before it touches the terminal — a bad body, a
 * session it cannot find, a full lane, a question that moved — and it is the
 * only failure after which "try again" is offered as such. Everything else is
 * `unknown`: no answer at all, an answer lost on the way back, a terminal that
 * failed part-way, a code this page has never heard of. The words may be on
 * the Mac; the card says it does not know, and sends a person to look rather
 * than to type them again.
 *
 * Across Clawdline Cloud the seam says what it knows about its own layers —
 * an envelope that never left this page, a relay that said the Mac was not
 * there — as `outcome` on the refusal (`cloud/relay-writer.ts`), and that wins.
 * A refusal from the Mac's own route carries no `outcome` and is read here by
 * its code, exactly as the same refusal from a daemon on this machine is.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

export type Outcome = "not_done" | "unknown"

/**
 * This daemon's refusals that are decided before any byte reaches a terminal
 * (`internal/transport/http/actions.go` `actionStatus`, `receipts.go`, the gate).
 * A code is added here only once the route that sends it has been read and
 * refuses before acting; an unknown code is never proof of anything.
 */
const NOT_DONE: ReadonlySet<string> = new Set([
  // The body or the route.
  "bad_request", "empty_text", "too_large", "not_found", "method_not_allowed",
  // The session.
  "session_not_found", "session_unknown", "backend_unsupported", "pictures_unavailable",
  // Its terminal's lane was full.
  "busy",
  // The device, and the machine's own switch.
  "forbidden", "unauthorized", "write_disabled",
  // A menu answer checked against the screen before anything was typed.
  "menu_moved", "menu_unreadable", "menu_unverified",
  // A close refused by what the session owes, or by not being able to say.
  "close_blocked", "closeability_unknown",
  // The request's receipt, refused before it was carried out.
  "idempotency_key_reused", "receipts_full", "store_busy", "store_unavailable",
])

/** One failed write, as the page has it. `status` is null when nothing answered. */
export interface FailedWrite {
  status: number | null
  code: string
  /** The seam's own `outcome`, when it sent one. */
  said?: unknown
}

export function outcomeOf(failure: FailedWrite): Outcome {
  if (failure.said === "not_done" || failure.said === "unknown") return failure.said
  if (failure.status === null) return "unknown"
  return NOT_DONE.has(failure.code) ? "not_done" : "unknown"
}
