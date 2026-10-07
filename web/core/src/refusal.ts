import type { CloseReason, Refusal } from "@clawdline/contract"

/** The gate/broker refusal envelope, whose metadata lives under `error`. */
export interface NestedRefusal {
  error: {
    code: string
    message?: string
    detail_key?: string
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
  detailKey?: string
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
  const detailKey = "detail_key" in body && typeof body.detail_key === "string"
    ? body.detail_key
    : typeof nested?.detail_key === "string"
      ? nested.detail_key
      : undefined
  const reasons = "reasons" in body && Array.isArray(body.reasons)
    ? body.reasons
    : Array.isArray(nested?.reasons)
      ? nested.reasons
      : []
  return {
    code: code as string,
    detail,
    detailKey,
    route: body.route ?? (typeof nested?.route === "string" ? nested.route : undefined),
    reasons,
    source: nested,
  }
}

/**
 * The refusal codes that mean "the machine answered, and its Clawdline is
 * older than this feature" — over Clawdline Cloud, where the copied client
 * says it in its own words (`legacy/js/net/cloud-client.js`):
 *
 * - `unknown_command`: the machine was asked the word and said it does not
 *   know it;
 * - `cloud_feature_unavailable`: a Mac already known to lack the word;
 * - `cloud_machine_unsupported`: the machine's descriptor lists its words and
 *   this one is not among them.
 *
 * `cloud_not_carried` is deliberately absent: it is *this console* not
 * carrying a route over Cloud, and updating the machine would change nothing.
 */
export const MACHINE_NEEDS_UPDATE_CODES: readonly string[] = Object.freeze([
  "unknown_command",
  "cloud_feature_unavailable",
  "cloud_machine_unsupported",
])

/**
 * A feature the machine does not have because its Clawdline is older than
 * this console (docs/updates.md). One shape, whichever request layer saw it.
 *
 * `code` is the wire code, kept so code that branches on it keeps working.
 * `version` is the machine's own version when the refusal carried one; absent
 * is unknown, and a screen then reads it from health instead.
 */
export interface MachineNeedsUpdate {
  kind: "machine_needs_update"
  code: string
  route?: string
  version?: string
}

/**
 * Whether a refusal says the machine needs an update for this feature.
 *
 * True for the daemon's own answer to a route it has no handler for — 501
 * `not_implemented` naming the `route` (`writeNoSuchRoute`,
 * internal/transport/http/write.go) — and for the three Cloud codes above.
 *
 * False for everything else, and on purpose for a 404 `not_found`: a daemon
 * from before 2026-10-06 answered an unknown sub-route that way, and it is
 * the same answer as a record that does not exist. Guessing would tell a
 * person to update a machine whose only fault is a deleted row.
 */
export function machineNeedsUpdate(status: number | undefined, code: unknown, route: unknown): boolean {
  if (typeof code !== "string") return false
  if (MACHINE_NEEDS_UPDATE_CODES.includes(code)) return true
  return status === 501 && code === "not_implemented" && typeof route === "string" && route.length > 0
}

/**
 * The typed "this machine needs an update" for anything a request layer
 * threw, or null when it is some other failure.
 *
 * It reads a `RefusalError`, a copied page's `jsonFetch` Error (`code`, and
 * `machineNeedsUpdate` set by `makeJSONFetch`), and any object with a string
 * `code` and optional `status`/`route`/`version`, so a module that does its
 * own `fetch` is understood without a second classifier.
 */
export function asMachineNeedsUpdate(err: unknown): MachineNeedsUpdate | null {
  if (typeof err !== "object" || err === null) return null
  const e = err as { code?: unknown; status?: unknown; route?: unknown; version?: unknown; machineNeedsUpdate?: unknown }
  if (typeof e.code !== "string") return null
  const marked = e.machineNeedsUpdate === true
  if (!marked && !machineNeedsUpdate(typeof e.status === "number" ? e.status : undefined, e.code, e.route)) return null
  return {
    kind: "machine_needs_update",
    code: e.code,
    ...(typeof e.route === "string" && e.route ? { route: e.route } : {}),
    ...(typeof e.version === "string" && e.version ? { version: e.version } : {}),
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
  readonly detailKey: string | undefined
  readonly status: number
  readonly route: string | undefined
  /** What a blocked close is blocked by. Empty for every other refusal. */
  readonly reasons: readonly CloseReason[]
  /**
   * The machine lacks this feature because its Clawdline is older
   * (`machineNeedsUpdate`). Decided here, in the constructor, so every request
   * layer that builds a RefusalError — the client, and each module that does
   * its own `fetch` — says it the same way without being edited.
   */
  readonly machineNeedsUpdate: boolean
  /** The machine's version, when the refusal named one (the Cloud relay does). */
  readonly version: string | undefined

  constructor(status: number, body: RefusalBody, route?: string) {
    const refusal = refusalFields(body)
    super(`${refusal.code}: ${refusal.detail}`)
    this.name = "RefusalError"
    this.code = refusal.code
    this.detail = refusal.detail
    this.detailKey = refusal.detailKey
    this.status = status
    this.route = refusal.route ?? route
    this.reasons = refusal.reasons
    // The body's own route, not the path asked: `not_implemented` counts only
    // when the daemon named the route it does not have.
    this.machineNeedsUpdate = machineNeedsUpdate(status, refusal.code, refusal.route)
    const version = (body as { version?: unknown }).version ?? refusal.source?.version
    this.version = typeof version === "string" && version ? version : undefined
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
  /**
   * The sentence for a feature the machine is too old for
   * (`machineNeedsUpdate`). Optional: without it the refusal's own detail is
   * the message, as before, and the Error is still marked.
   */
  machineNeedsUpdate?: string
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
      const refusalBody = isRefusal(body) ? body : null
      const refusal = refusalBody
        ? refusalFields(refusalBody)
        : {
            code: "http_" + response.status,
            detail: response.statusText || config.words.requestFailed,
            detailKey: undefined,
            source: null,
          }
      const needsUpdate = machineNeedsUpdate(response.status, refusal.code, "route" in refusal ? refusal.route : undefined)
      const message = needsUpdate && config.words.machineNeedsUpdate ? config.words.machineNeedsUpdate : refusal.detail || refusal.code
      const failed = new Error(message) as Error & Record<string, unknown>
      failed.code = refusal.code
      if (refusalBody) {
        failed.detail = refusal.detail
        if (refusal.detailKey) failed.detailKey = refusal.detailKey
      }
      if (needsUpdate) {
        failed.machineNeedsUpdate = true
        if ("route" in refusal && refusal.route) failed.route = refusal.route
      }
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
