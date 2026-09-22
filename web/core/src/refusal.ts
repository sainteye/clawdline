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
  source: Record<string, unknown> | null
} {
  const error = body.error
  const nested = typeof error === "object" ? error as Record<string, unknown> : null
  const code = typeof error === "string" ? error : nested!.code
  const detail: string = "detail" in body && typeof body.detail === "string"
    ? body.detail
    : typeof nested?.message === "string"
      ? nested.message
      : code as string
  const reasons = "reasons" in body && Array.isArray(body.reasons)
    ? body.reasons
    : Array.isArray(nested?.reasons)
      ? nested.reasons
      : []
  return {
    code: code as string,
    detail,
    route: body.route ?? (typeof nested?.route === "string" ? nested.route : undefined),
    reasons,
    source: nested,
  }
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

/** The three host-owned sentences the legacy JSON transport can need. */
export interface JSONFetchWords {
  offline: string
  requestFailed: string
  notJSON: string
}

/** One route-specific refusal field copied onto the Error consumed by a legacy page. */
export interface JSONFetchErrorField {
  source: string
  target: string
  type: "string" | "number"
}

/**
 * The small differences between the copied pages' otherwise identical JSON
 * transports. The transport is shared; these are the behaviours its callers
 * intentionally retain.
 */
export interface JSONFetchConfig {
  words: JSONFetchWords
  defaults?: RequestInit
  refusalFields?: readonly JSONFetchErrorField[]
  invalidJSONCode?: string
  /** Documents accepts any parsed JSON value; the other copied clients require a truthy body. */
  allowFalsyJSON?: boolean
}

export type JSONFetch = <A = Record<string, unknown>>(
  path: string,
  init?: RequestInit,
  onResponse?: (response: Response) => void,
) => Promise<A>

/**
 * Build one of the copied console's `jsonFetch` functions.
 *
 * Every instance uses the same network, JSON and refusal implementation. A
 * caller supplies only its catalog sentences, request defaults and the few
 * refusal fields its old page reads (`app`, `reason`, or `tries_left`). Both
 * wire refusal envelopes stay recognised here, beside `isRefusal`, so adding a
 * bridge cannot quietly turn a named daemon refusal into `unexpected_error`.
 */
export function makeJSONFetch(config: JSONFetchConfig): JSONFetch {
  return async <A>(path: string, init?: RequestInit, onResponse?: (response: Response) => void): Promise<A> => {
    let response: Response
    try {
      response = await fetch(path, { ...(config.defaults ?? {}), ...(init ?? {}) })
    } catch {
      const offline = new Error(config.words.offline) as Error & { code?: string }
      offline.code = "offline"
      throw offline
    }
    onResponse?.(response)

    const text = await response.text()
    let body: unknown = null
    try {
      body = text ? JSON.parse(text) : null
    } catch {
      /* A non-JSON refusal falls through to the status; a success is rejected below. */
    }

    if (!response.ok) {
      const refusal = isRefusal(body)
        ? refusalFields(body)
        : {
            code: "http_" + response.status,
            detail: response.statusText || config.words.requestFailed,
            source: null,
          }
      const failed = new Error(refusal.detail || refusal.code) as Error & Record<string, unknown>
      failed.code = refusal.code
      for (const field of config.refusalFields ?? []) {
        const value = refusal.source?.[field.source]
        if (typeof value === field.type) failed[field.target] = value
      }
      throw failed
    }

    const invalid = config.allowFalsyJSON ? body === null : !body
    if (invalid) {
      const failed = new Error(config.words.notJSON) as Error & { code?: string }
      if (config.invalidJSONCode) failed.code = config.invalidJSONCode
      throw failed
    }
    return body as A
  }
}
