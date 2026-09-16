import type { Refusal } from "@clawdline/contract"

/**
 * A refusal the daemon returned, kept whole.
 *
 * `code` is the machine-readable half and the only part anything may branch
 * on. `detail` is for a person and must never be parsed — a UI that switched
 * on its wording would break the first time the wording improved.
 */
export class RefusalError extends Error {
  readonly code: string
  readonly detail: string
  readonly status: number
  readonly route: string | undefined

  constructor(status: number, body: Refusal, route?: string) {
    super(`${body.error}: ${body.detail}`)
    this.name = "RefusalError"
    this.code = body.error
    this.detail = body.detail
    this.status = status
    this.route = body.route ?? route
  }
}

/**
 * A failure that never reached the daemon, or reached it and came back as
 * something other than a refusal.
 *
 * It is a separate class from RefusalError on purpose. "The daemon said no"
 * and "nobody answered" are different facts, and a screen that showed them the
 * same way would teach a person to distrust both.
 */
export class TransportError extends Error {
  readonly cause: unknown
  constructor(message: string, cause?: unknown) {
    super(message)
    this.name = "TransportError"
    this.cause = cause
  }
}

export function isRefusal(value: unknown): value is Refusal {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as Refusal).error === "string" &&
    typeof (value as Refusal).detail === "string"
  )
}
