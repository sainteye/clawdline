/* What an operator has to act on now: `GET {api}/su/v1/health/attention`.
 *
 * The Cloud decides which incidents need someone; this page only checks the answer's
 * shape and refuses what it cannot trust. A body that fails any check is a failure,
 * never "nothing to do": an unknown schema, an unknown action, or a link that does
 * not point at one of this console's own incident screens is not shown as zero.
 * Nothing is imported at run time, so node's strip-types tests load it as it is. */
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { HEALTH_UI_LIMITS } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { INCIDENT_ID } from "./stored.ts"

export const ATTENTION_SCHEMA = "clawdline.health_attention.v1"
export const ATTENTION_ACTIONS = ["investigate", "review_fix", "review_result"] as const
export type AttentionAction = (typeof ATTENTION_ACTIONS)[number]
/** Why the Cloud listed an item (Cloud rule `attention/1`), each with the one action it asks for.
 *  A reason this console does not know reads as `unknown`, so its code is never shown as a sentence;
 *  a known reason paired with another action is a contract failure. */
export const ATTENTION_REASONS = {
  no_investigation: "investigate",
  fix_prepared: "review_fix",
  verified_recovered: "review_result",
  verification_failed: "review_result",
} as const satisfies Record<string, AttentionAction>
export type AttentionReason = keyof typeof ATTENTION_REASONS | "unknown"

export type AttentionItem = {
  id: string
  incident_id: string
  incident_revision: number
  action: AttentionAction
  reason: AttentionReason
  href: string
}

/** `complete: false` means the Cloud could not list everything; the shown items are a floor. */
export type Attention =
  | { state: "ok"; items: AttentionItem[]; complete: boolean }
  | { state: "unavailable" }
  | { state: "failed"; code: AttentionFailure }

export type AttentionFailure = "signed_out" | "forbidden" | "network" | "timeout" | "http_error" | "contract" | "body_too_large"

function str(v: unknown, max = 1024): v is string {
  return typeof v === "string" && v.length > 0 && v.length <= max
}

/** The only links accepted: this console's own incident screen for the item's incident. */
export function attentionHref(incident: string): string {
  return `/admin/health/incidents/${encodeURIComponent(incident)}`
}

function parseItem(v: unknown): AttentionItem | null {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return null
  const o = v as Record<string, unknown>
  if (!str(o.id, 256) || !str(o.incident_id, 128) || !INCIDENT_ID.test(o.incident_id)) return null
  if (typeof o.incident_revision !== "number" || !Number.isSafeInteger(o.incident_revision) || o.incident_revision < 1) return null
  if (typeof o.action !== "string" || !(ATTENTION_ACTIONS as readonly string[]).includes(o.action)) return null
  if (!str(o.reason, 2048) || !str(o.href, 512)) return null
  const known = Object.hasOwn(ATTENTION_REASONS, o.reason) ? o.reason as keyof typeof ATTENTION_REASONS : null
  if (known !== null && ATTENTION_REASONS[known] !== o.action) return null
  // Same-origin path only, and exactly the incident it names: no scheme, host, query or fragment.
  if (o.href !== attentionHref(o.incident_id)) return null
  return { id: o.id, incident_id: o.incident_id, incident_revision: o.incident_revision, action: o.action as AttentionAction, reason: known ?? "unknown", href: o.href }
}

/** Null when the body is not exactly the declared contract. */
export function parseAttention(body: unknown): Attention | null {
  if (typeof body !== "object" || body === null || Array.isArray(body)) return null
  const o = body as Record<string, unknown>
  if (o.schema !== ATTENTION_SCHEMA || typeof o.complete !== "boolean") return null
  if (o.status === "unavailable") return o.items === null || o.items === undefined ? { state: "unavailable" } : null
  if (o.status !== "ok" || !Array.isArray(o.items)) return null
  const items: AttentionItem[] = []
  const seen = new Set<string>()
  for (const raw of o.items) {
    const item = parseItem(raw)
    // One bad row makes the whole answer untrustworthy; it is not silently dropped.
    if (item === null || seen.has(item.id)) return null
    seen.add(item.id)
    items.push(item)
  }
  return { state: "ok", items, complete: o.complete }
}

/** One bounded read. Never throws; every failure is typed. */
export async function readAttention(apiOrigin: string, get: typeof fetch, signal?: AbortSignal): Promise<Attention> {
  const ctl = new AbortController()
  const abort = () => ctl.abort()
  signal?.addEventListener("abort", abort, { once: true })
  const timer = setTimeout(abort, HEALTH_UI_LIMITS.requestMs)
  try {
    let res: Response
    try {
      res = await get(`${apiOrigin}/su/v1/health/attention`, { credentials: "include", cache: "no-store", signal: ctl.signal })
    } catch {
      return { state: "failed", code: ctl.signal.aborted && !signal?.aborted ? "timeout" : "network" }
    }
    if (res.status === 401) return { state: "failed", code: "signed_out" }
    if (res.status === 403 || res.status === 404) return { state: "failed", code: "forbidden" }
    if (!res.ok) return { state: "failed", code: "http_error" }
    const declared = Number(res.headers.get("content-length"))
    if (Number.isFinite(declared) && declared > HEALTH_UI_LIMITS.bodyBytes) return { state: "failed", code: "body_too_large" }
    let text: string
    try { text = await res.text() } catch { return { state: "failed", code: ctl.signal.aborted ? "timeout" : "network" } }
    if (text.length > HEALTH_UI_LIMITS.bodyBytes) return { state: "failed", code: "body_too_large" }
    let body: unknown
    try { body = JSON.parse(text) } catch { return { state: "failed", code: "contract" } }
    return parseAttention(body) ?? { state: "failed", code: "contract" }
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener("abort", abort)
  }
}

/** The sidebar mark: only actionable items the Cloud listed. Failures and waiting are not a count. */
export function attentionCount(a: Attention | null): number | null {
  return a !== null && a.state === "ok" ? a.items.length : null
}

/** One shared reader per Cloud origin, however many shells mount: a visible page reads once and
 *  then at the existing live cadence; a hidden page does not read; a failure waits the same
 *  interval, so failures never read faster than successes. */
export type AttentionWatch = { subscribe: (fn: (a: Attention | null) => void) => () => void; current: () => Attention | null }

type Doc = { readonly visibilityState: string; addEventListener: (t: "visibilitychange", fn: () => void) => void; removeEventListener: (t: "visibilitychange", fn: () => void) => void }

export function attentionWatch(apiOrigin: string, get: typeof fetch, doc: Doc | null = typeof document === "undefined" ? null : document, everyMs: number = HEALTH_UI_LIMITS.refreshMs): AttentionWatch {
  let value: Attention | null = null
  let lastRead = 0
  let inFlight: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  const listeners = new Set<(a: Attention | null) => void>()
  const visible = () => doc === null || doc.visibilityState === "visible"
  const schedule = () => {
    if (timer !== null) clearTimeout(timer)
    timer = null
    if (listeners.size === 0 || !visible()) return
    timer = setTimeout(read, Math.max(0, lastRead + everyMs - Date.now()))
  }
  const read = () => {
    timer = null
    if (inFlight || listeners.size === 0 || !visible()) return
    const ctl = new AbortController()
    inFlight = ctl
    lastRead = Date.now()
    void readAttention(apiOrigin, get, ctl.signal).then((next) => {
      if (inFlight === ctl) inFlight = null
      if (ctl.signal.aborted) return
      value = next
      for (const fn of listeners) fn(value)
      schedule()
    })
  }
  const onVisibility = () => schedule()
  return {
    current: () => value,
    subscribe(fn) {
      listeners.add(fn)
      if (listeners.size === 1) { doc?.addEventListener("visibilitychange", onVisibility); schedule() }
      return () => {
        listeners.delete(fn)
        if (listeners.size > 0) return
        doc?.removeEventListener("visibilitychange", onVisibility)
        if (timer !== null) clearTimeout(timer)
        timer = null
        inFlight?.abort()
        inFlight = null
      }
    },
  }
}
