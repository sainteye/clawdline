import type { CloseReason, Refusal } from "@clawdline/contract"

/** The gate/broker refusal envelope, whose metadata lives under `error`. */
export interface NestedRefusal {
  error: {
    code: string
    message?: string
    route?: string
    reasons?: CloseReason[]
    [key: string]: unknown
  }
  route?: string
}

/** Either refusal spelling that may arrive on the wire. */
export type RefusalBody =
  | (Omit<Refusal, "detail"> & { detail?: string; reasons?: CloseReason[] })
  | NestedRefusal

function refusalFields(body: RefusalBody): {
  code: string
  detail: string
  route?: string
  reasons: readonly CloseReason[]
} {
  const error = body.error
  const nested = typeof error === "object" ? error : null
  const code = typeof error === "string" ? error : nested!.code
  const detail = "detail" in body && typeof body.detail === "string"
    ? body.detail
    : typeof nested?.message === "string"
      ? nested.message
      : code
  const reasons = "reasons" in body && Array.isArray(body.reasons)
    ? body.reasons
    : Array.isArray(nested?.reasons)
      ? nested.reasons
      : []
  return { code, detail, route: body.route ?? nested?.route, reasons }
}

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
  /** What a blocked close is blocked by. Empty for every other refusal. */
  readonly reasons: readonly CloseReason[]

  constructor(status: number, body: RefusalBody, route?: string) {
    const refusal = refusalFields(body)
    super(`${refusal.code}: ${refusal.detail}`)
    this.name = "RefusalError"
    this.code = refusal.code
    this.detail = refusal.detail
    this.status = status
    this.route = refusal.route ?? route
    this.reasons = refusal.reasons
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

export function isRefusal(value: unknown): value is RefusalBody {
  if (typeof value !== "object" || value === null) return false
  const error = (value as { error?: unknown }).error
  if (typeof error === "string") return true
  return typeof error === "object" && error !== null && typeof (error as { code?: unknown }).code === "string"
}
