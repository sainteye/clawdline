/* Loads the stored connection-health evidence a screen needs from the Cloud's
 * operator reads, `GET {api}/su/v1/health/…`, with the page's own session cookie.
 *
 * One load is a fixed plan of at most three reads (never one per row), run at
 * most two at a time, shared with the tab's other health readers, each
 * under its own deadline and all under one overall budget, cancelled together.
 * A read that fails keeps its own typed reason and never discards a read that
 * succeeded. A refusal of the session (401, or the operator surface answering
 * 404 `not_found` to someone who is not an operator) ends the whole load and
 * clears everything held, so one account's evidence never shows under another.
 * Nothing is imported at run time, so node's strip-types tests load it as it is. */
import type { HealthRoute } from "./route.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { windowBounds } from "./route.ts"
import type { EventsBody, IncidentDetailBody, IncidentListBody, IncidentRow, OutcomesBody, OverviewBody, SourcesBody, StoredEvent } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseEvents, parseIncidentDetail, parseIncidentList, parseOutcomes, parseOverview, parseSources, StoredContractError, STORED_BOUNDS } from "./stored.ts"
import type { ProbesBody } from "./probes.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseProbes } from "./probes.ts"
import type { AggregatesBody, TrendRange } from "./aggregates.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseAggregates, trendWindow } from "./aggregates.ts"

/** Bounds of one load. Registered as console.health_ui_* (internal/domain/capacity). */
export const HEALTH_UI_LIMITS = {
  /** One read's deadline; the Cloud's own request deadline is 5 s. */
  requestMs: 6_000,
  /** The whole load, every read included. */
  budgetMs: 12_000,
  /** Shared tab admission stays compatible with the previous two-slot API. */
  parallel: 2,
  /** Waiting reads across page loads and attention; excess is typed backpressure. */
  queuedReads: 8,
  /** Maximum server-directed pause; refreshes never retry a read immediately. */
  retryAfterMs: 60_000,
  /** Reads one screen's load may issue. */
  readsPerLoad: 3,
  /** Live refresh cadence, one load outstanding. */
  refreshMs: 15_000,
  /** One response body. The Cloud pages at 1 MiB; anything far past it is refused unread. */
  bodyBytes: 1_536 * 1024,
  /** Event rows asked for per page (the Cloud's own maximum is 200). */
  pageRows: STORED_BOUNDS.pageRows,
  /** Pages of one event list held at once, the first included. */
  eventPages: 5,
  /** Incident rows asked for per list read (the Cloud's default page; its maximum is 200). */
  incidentRows: 50,
  /** One incident export body: the Cloud refuses to export past 1 MiB, so more is not its bundle. */
  incidentExportBytes: 1024 * 1024,
  /** The incident list's window over `opened_at` (the Cloud's own maximum, 7 days). */
  incidentWindowMs: 7 * 86_400_000,
  /** Pages of the incident list held at once, the first included (250 incidents at 50 a page). */
  incidentPages: 5,
  /** Trend lanes drawn as charts (each up to 360 buckets); the rest stay in the table. */
  trendLanes: 12,
  /** Trend buckets listed to investigate, newest first; the rest are counted. */
  trendSignals: 20,
} as const

type WaitingRead = { start: () => void; cancel: () => void }
type ReadAdmission = { active: number; waiting: WaitingRead[] }
const admissions = new Map<string, ReadAdmission>()

export class HealthReadBusy extends Error {
  readonly code = "rate_limited"
  constructor() { super("health_read_busy") }
}

/** One FIFO per origin, with no cached answer or credentials. Abort removes a queued read;
 * an active read keeps its slot until the underlying fetch actually settles. */
export function healthGet(get: typeof fetch, url: string, init: RequestInit): Promise<Response> {
  const origin = new URL(url).origin
  const signal = init.signal
  if (signal?.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"))
  let pool = admissions.get(origin)
  if (!pool) { pool = { active: 0, waiting: [] }; admissions.set(origin, pool) }
  const current = pool
  if (current.active >= HEALTH_UI_LIMITS.parallel && current.waiting.length >= HEALTH_UI_LIMITS.queuedReads)
    return Promise.reject(new HealthReadBusy())
  return new Promise<Response>((resolve, reject) => {
    const cleanup = () => {
      if (current.active === 0 && current.waiting.length === 0) admissions.delete(origin)
    }
    const finish = () => {
      current.active--
      current.waiting.shift()?.start()
      cleanup()
    }
    const waiting: WaitingRead = {
      start() {
        signal?.removeEventListener("abort", waiting.cancel)
        current.active++
        // Synchronous fetch failures have the same release boundary as rejected promises.
        void Promise.resolve().then(() => get(url, init)).then(resolve, reject).finally(finish)
      },
      cancel() {
        const at = current.waiting.indexOf(waiting)
        if (at < 0) return
        current.waiting.splice(at, 1)
        signal?.removeEventListener("abort", waiting.cancel)
        reject(new DOMException("Aborted", "AbortError"))
        cleanup()
      },
    }
    if (current.active < HEALTH_UI_LIMITS.parallel) waiting.start()
    else { current.waiting.push(waiting); signal?.addEventListener("abort", waiting.cancel, { once: true }) }
  })
}

/** Retain a bounded Retry-After hint; a hint never causes an automatic fetch retry. */
export function retryAfterMs(response: Response, now = Date.now()): number | undefined {
  const value = response.headers.get("retry-after")
  if (value === null) return undefined
  const delay = /^\d+$/.test(value) ? Number(value) * 1000 : Date.parse(value) - now
  return Number.isFinite(delay) ? Math.min(HEALTH_UI_LIMITS.retryAfterMs, Math.max(0, delay)) : undefined
}

export type PartName = "overview" | "outcomes" | "sources" | "events" | "trace" | "incidents" | "incident" | "aggregates" | "probes"
export type PartBody = {
  probes: ProbesBody
  aggregates: AggregatesBody
  overview: OverviewBody
  outcomes: OutcomesBody
  sources: SourcesBody
  events: EventsBody
  trace: EventsBody
  incidents: IncidentListBody
  incident: IncidentDetailBody
}

export type StoredFailure =
  | "timeout" | "budget" | "network" | "body_too_large" | "contract_schema" | "contract_shape" | "contract_bound"
  | "unsupported" | "rate_limited" | "unavailable" | "server_deadline" | "audit_unavailable" | "machine_not_found"
  | "bad_cursor" | "invalid_request" | "http_error"
  | "incident_not_found" | "revision_not_found" | "incident_expired" | "bad_origin" | "revision_cap"

export type Refusal = "signed_out" | "forbidden"

/** `expiredAt`: the Cloud's `details.expired_at` on `410 incident_expired`, so the page can say when. */
export type PartResult<N extends PartName = PartName> = { ok: true; body: PartBody[N] } | { ok: false; code: StoredFailure; expiredAt?: string | null; retryAfterMs?: number }

export type Read = { part: PartName; path: string; params: Record<string, string> }

/** What a screen asks for. Fixed per screen: the same few reads whatever the data holds. */
export function planFor(route: HealthRoute, now: number): { window: { from: string; to: string }; reads: Read[] } {
  const window = windowBounds(route.scope, now)
  const win = { from: window.from, to: window.to }
  const e = encodeURIComponent
  const rows = String(HEALTH_UI_LIMITS.pageRows)
  // Incidents are listed by when they opened, over the Cloud's 7-day window ending where this one
  // ends, so a pinned page lists the same incidents on every load.
  const incidentList: Read = { part: "incidents", path: "incidents", params: { from: new Date(Date.parse(window.to) - HEALTH_UI_LIMITS.incidentWindowMs).toISOString(), to: window.to, limit: String(HEALTH_UI_LIMITS.incidentRows) } }
  let reads: Read[]
  if (route.screen === "M06" && route.probes) {
    // One read with no parameters: the latest retained probe evidence, service scope. No window,
    // pin or account is sent, because the Cloud would honour none of them.
    return { window, reads: [{ part: "probes", path: "probes", params: {} }] }
  }
  const trend = route.screen === "M01" || route.screen === "M03" || route.screen === "M06" ? route.trend ?? null : null
  if (trend !== null && (route.screen === "M01" || route.screen === "M03" || route.screen === "M06")) {
    // The trend view replaces the screen's raw reads instead of adding a fourth: the same
    // per-load cap, concurrency and budget hold. 30 days is the Cloud's daily buckets, never a
    // client-side scan of raw rows.
    const t = trendWindow(trend as TrendRange, Date.parse(window.to))
    const agg: Record<string, string> = { from: t.from, to: t.to, bucket: t.bucket }
    if (route.trendMachine) agg.machine_id = route.trendMachine
    if (route.screen === "M03" && route.source) agg.source = route.source
    if (route.screen === "M03" && route.sourceId) agg.source_id = route.sourceId
    const aggregates: Read = { part: "aggregates", path: "aggregates", params: agg }
    // Incidents opened over the 7 days ending where the trend ends, for the "investigate" links.
    const incidents: Read = { ...incidentList, params: { ...incidentList.params, from: new Date(Date.parse(t.to) - HEALTH_UI_LIMITS.incidentWindowMs).toISOString(), to: new Date(Math.min(Date.parse(t.to), Date.parse(window.to))).toISOString() } }
    reads = route.screen === "M01" ? [aggregates, { part: "overview", path: "overview", params: win }, incidents]
      : [aggregates, { part: "overview", path: "overview", params: win }, { part: "sources", path: "sources", params: {} }]
    if (route.scope.account !== null) reads = reads.map((r) => ({ ...r, params: { ...r.params, account: route.scope.account! } }))
    return { window: { from: t.from, to: t.to }, reads: reads.slice(0, HEALTH_UI_LIMITS.readsPerLoad) }
  }
  switch (route.screen) {
    case "M01":
      reads = [{ part: "overview", path: "overview", params: win }, { part: "outcomes", path: "outcomes", params: win }, incidentList]
      break
    case "M02":
      reads = route.machine === null
        ? [{ part: "sources", path: "sources", params: {} }, { part: "outcomes", path: "outcomes", params: win }]
        : [
            { part: "sources", path: "sources", params: { machine_id: route.machine } },
            { part: "outcomes", path: "outcomes", params: { ...win, machine_id: route.machine } },
            { part: "events", path: "events", params: { ...win, machine_id: route.machine, limit: rows } },
          ]
      break
    case "M03": {
      // The Cloud narrows the events read itself (contract §3); its cursor binds the filter.
      const filter: Record<string, string> = {}
      if (route.source) filter.source = route.source
      if (route.sourceId) filter.source_id = route.sourceId
      reads = [{ part: "overview", path: "overview", params: win }, { part: "sources", path: "sources", params: {} }, { part: "events", path: "events", params: { ...win, ...filter, limit: rows } }]
      break
    }
    case "M04":
      reads = [{ part: "trace", path: `trace/${e(route.trace)}`, params: { ...win, limit: rows } }, { part: "outcomes", path: "outcomes", params: win }, incidentList]
      break
    case "M05":
      reads = route.incident === null
        ? [incidentList, { part: "outcomes", path: "outcomes", params: win }]
        // One revision of one incident; no revision asks for the latest.
        : [{ part: "incident", path: `incidents/${e(route.incident)}`, params: route.revision ? { revision: String(route.revision) } : {} }]
      break
    case "M06":
      reads = [{ part: "overview", path: "overview", params: win }, { part: "sources", path: "sources", params: {} }, { part: "outcomes", path: "outcomes", params: win }]
      break
    case "evidence":
      reads = [
        { part: "outcomes", path: "outcomes", params: win },
        route.trace === null ? { part: "events", path: "events", params: { ...win, limit: rows } } : { part: "trace", path: `trace/${e(route.trace)}`, params: { ...win, limit: rows } },
        { part: "sources", path: "sources", params: {} },
      ]
      break
  }
  // The account filter narrows an operator read; it is never authority (contract §1).
  if (route.scope.account !== null) reads = reads.map((r) => ({ ...r, params: { ...r.params, account: route.scope.account! } }))
  return { window, reads: reads.slice(0, HEALTH_UI_LIMITS.readsPerLoad) }
}

export function readURL(apiOrigin: string, read: Read): string {
  const q = new URLSearchParams(read.params).toString()
  return `${apiOrigin}/su/v1/health/${read.path}${q ? `?${q}` : ""}`
}

const PARSERS: { [N in PartName]: (body: unknown) => PartBody[N] } = {
  aggregates: parseAggregates, probes: parseProbes, overview: parseOverview, outcomes: parseOutcomes, sources: parseSources, events: parseEvents, trace: parseEvents, incidents: parseIncidentList, incident: parseIncidentDetail,
}

/** The server's typed refusal codes this page explains; anything else is its HTTP class. */
export function failureFor(status: number, code: string | null): StoredFailure | Refusal {
  if (status === 401) return "signed_out"
  // The operator surface answers 404 not_found to everyone who is not an operator (requireSuperuser).
  if (status === 403 || (status === 404 && (code === "not_found" || code === null))) return "forbidden"
  switch (code) {
    case "unsupported_capability": return "unsupported"
    case "health_busy": return "rate_limited"
    case "health_deadline_exceeded": return "server_deadline"
    case "health_store_unavailable": return "unavailable"
    case "audit_unavailable": return "audit_unavailable"
    case "machine_not_found": return "machine_not_found"
    case "bad_cursor": return "bad_cursor"
    case "invalid_field": case "invalid_window": case "window_too_wide": case "unknown_field": return "invalid_request"
    case "incident_not_found": return "incident_not_found"
    case "revision_not_found": return "revision_not_found"
    case "incident_expired": return "incident_expired"
    case "bad_origin": return "bad_origin"
    case "revision_cap": return "revision_cap"
  }
  if (status === 501) return "unsupported"
  if (status === 429) return "rate_limited"
  if (status >= 500) return "unavailable"
  return "http_error"
}

const isRefusal = (v: StoredFailure | Refusal): v is Refusal => v === "signed_out" || v === "forbidden"

async function boundedText(res: Response): Promise<string | null> {
  const declared = Number(res.headers.get("content-length"))
  if (Number.isFinite(declared) && declared > HEALTH_UI_LIMITS.bodyBytes) return null
  const text = await res.text()
  return new TextEncoder().encode(text).length > HEALTH_UI_LIMITS.bodyBytes ? null : text
}

/** The error code, and `details.expired_at` when the Cloud gave one, from a refused read's body. */
export function errorInfo(text: string | null): { code: string | null; expiredAt: string | null } {
  if (text === null) return { code: null, expiredAt: null }
  try {
    const error = (JSON.parse(text) as { error?: { code?: unknown; details?: { expired_at?: unknown } } })?.error
    const code = typeof error?.code === "string" && /^[a-z_]{1,64}$/.test(error.code) ? error.code : null
    const at = error?.details?.expired_at
    return { code, expiredAt: typeof at === "string" && at.length <= 40 && Number.isFinite(Date.parse(at)) ? at : null }
  } catch {
    return { code: null, expiredAt: null }
  }
}

type One = { ok: true; body: PartBody[PartName] } | { ok: false; code: StoredFailure; expiredAt?: string | null; retryAfterMs?: number } | { refused: Refusal }

async function readOne(get: typeof fetch, url: string, part: PartName, signal: AbortSignal): Promise<One> {
  const res = await healthGet(get, url, { credentials: "include", headers: { accept: "application/json" }, signal })
  if (!res.ok) {
    const info = errorInfo(await boundedText(res).catch(() => null))
    const failure = failureFor(res.status, info.code)
    if (isRefusal(failure)) return { refused: failure }
    if (failure === "rate_limited") return { ok: false, code: failure, retryAfterMs: retryAfterMs(res) }
    return failure === "incident_expired" ? { ok: false, code: failure, expiredAt: info.expiredAt } : { ok: false, code: failure }
  }
  const text = await boundedText(res)
  if (text === null) return { ok: false, code: "body_too_large" }
  let json: unknown
  try {
    json = JSON.parse(text)
  } catch {
    return { ok: false, code: "contract_shape" }
  }
  try {
    return { ok: true, body: PARSERS[part](json) }
  } catch (err) {
    if (err instanceof StoredContractError) return { ok: false, code: err.code }
    throw err
  }
}

export type LoadOutcome =
  | { kind: "aborted" }
  | { kind: "refused"; refusal: Refusal }
  | { kind: "parts"; window: { from: string; to: string }; parts: Partial<Record<PartName, PartResult>> }

export type LoadOptions = { requestMs?: number; budgetMs?: number; parallel?: number }

/** Runs reads with bounded concurrency, one deadline each and one budget for all. */
export async function runReads(get: typeof fetch, apiOrigin: string, reads: Read[], outer: AbortSignal, opts: LoadOptions = {}): Promise<{ kind: "aborted" } | { kind: "refused"; refusal: Refusal } | { kind: "parts"; parts: Partial<Record<PartName, PartResult>> }> {
  const requestMs = opts.requestMs ?? HEALTH_UI_LIMITS.requestMs
  const budgetMs = opts.budgetMs ?? HEALTH_UI_LIMITS.budgetMs
  const parallel = Math.max(1, Math.min(opts.parallel ?? HEALTH_UI_LIMITS.parallel, HEALTH_UI_LIMITS.parallel))
  const all = new AbortController()
  let budgetSpent = false
  let refusal: Refusal | null = null
  const budget = setTimeout(() => { budgetSpent = true; all.abort() }, budgetMs)
  const forward = () => all.abort()
  if (outer.aborted) all.abort()
  else outer.addEventListener("abort", forward, { once: true })
  const parts: Partial<Record<PartName, PartResult>> = {}
  const queue = reads.slice(0, HEALTH_UI_LIMITS.readsPerLoad)

  const one = async (read: Read) => {
    const mine = new AbortController()
    let timedOut = false
    const timer = setTimeout(() => { timedOut = true; mine.abort() }, requestMs)
    const stop = () => mine.abort()
    if (all.signal.aborted) mine.abort()
    else all.signal.addEventListener("abort", stop, { once: true })
    try {
      const result = await Promise.race([
        readOne(get, readURL(apiOrigin, read), read.part, mine.signal),
        new Promise<never>((_, reject) => mine.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true })),
      ])
      if ("refused" in result) {
        refusal ??= result.refused
        all.abort()
        return
      }
      parts[read.part] = result
    } catch (error) {
      parts[read.part] = { ok: false, code: timedOut ? "timeout" : budgetSpent ? "budget" : error instanceof HealthReadBusy ? "rate_limited" : "network" }
    } finally {
      clearTimeout(timer)
      all.signal.removeEventListener("abort", stop)
    }
  }

  const workers = Array.from({ length: Math.min(parallel, queue.length) }, async () => {
    while (queue.length > 0 && !all.signal.aborted) await one(queue.shift()!)
  })
  await Promise.all(workers)
  // Reads never started because the budget ran out still say so, rather than vanishing.
  for (const read of queue) parts[read.part] ??= { ok: false, code: "budget" }
  clearTimeout(budget)
  outer.removeEventListener("abort", forward)
  if (outer.aborted) return { kind: "aborted" }
  if (refusal !== null) return { kind: "refused", refusal }
  return { kind: "parts", parts }
}

export async function loadStored(get: typeof fetch, apiOrigin: string, route: HealthRoute, now: number, outer: AbortSignal, opts?: LoadOptions): Promise<LoadOutcome> {
  const plan = planFor(route, now)
  const result = await runReads(get, apiOrigin, plan.reads, outer, opts)
  return result.kind === "parts" ? { kind: "parts", window: plan.window, parts: result.parts } : result
}

/** The next page of the same event list: same filters and the same frozen window, or the Cloud
 *  refuses the cursor (`bad_cursor`). */
export function olderRead(first: Read, window: { from: string; to: string }, cursor: string): Read {
  return { ...first, params: { ...first.params, from: window.from, to: window.to, cursor } }
}

/* ---- what the page holds between loads ---------------------------------- */

export type PartState =
  | { status: "ok"; body: PartBody[PartName]; at: number; failure: StoredFailure | null }
  | { status: "failed"; code: StoredFailure; expiredAt?: string | null }

export type StoredState = {
  key: string
  status: "loading" | "ready" | "refused"
  refusal: Refusal | null
  refreshing: boolean
  window: { from: string; to: string } | null
  parts: Partial<Record<PartName, PartState>>
  /** Pages after the first, newest first, each from the frozen window of the first. */
  older: StoredEvent[][]
  olderCursor: string | null
  olderFailure: StoredFailure | null
  olderBusy: boolean
  /** Incident-list pages after the first, from the same frozen window and filter (the Cloud's
   *  cursor hashes both, so a moved window starts again). */
  incOlder: IncidentRow[][]
  incCursor: string | null
  incFailure: StoredFailure | null
  incBusy: boolean
}

export type StoredAction =
  | { type: "reset"; key: string }
  | { type: "start" }
  | { type: "loaded"; outcome: LoadOutcome; at: number }
  | { type: "older_start" }
  | { type: "older_loaded"; result: PartResult; at: number }
  | { type: "older_refused"; refusal: Refusal }
  | { type: "inc_older_start" }
  | { type: "inc_older_loaded"; result: PartResult; at: number }

export const emptyStored = (key: string): StoredState => ({
  key, status: "loading", refusal: null, refreshing: true, window: null, parts: {}, older: [], olderCursor: null, olderFailure: null, olderBusy: false,
  incOlder: [], incCursor: null, incFailure: null, incBusy: false,
})

/** Failures that mean the thing itself is gone, so an earlier answer must not stay on screen. */
const GONE: ReadonlySet<StoredFailure> = new Set(["incident_not_found", "revision_not_found", "incident_expired", "machine_not_found"])

const listPart = (parts: StoredState["parts"]): PartState | undefined => parts.trace ?? parts.events

export function storedReducer(state: StoredState, action: StoredAction): StoredState {
  switch (action.type) {
    case "reset":
      return emptyStored(action.key)
    case "start":
      return state.status === "refused" ? { ...emptyStored(state.key) } : { ...state, refreshing: true }
    case "loaded": {
      const o = action.outcome
      if (o.kind === "aborted") return { ...state, refreshing: false }
      // A refused session clears every figure at once; nothing from before is kept to flash.
      if (o.kind === "refused") return { ...emptyStored(state.key), status: "refused", refusal: o.refusal, refreshing: false }
      const parts: StoredState["parts"] = {}
      for (const [name, r] of Object.entries(o.parts) as [PartName, PartResult][]) {
        const prev = state.parts[name]
        if (r.ok) parts[name] = { status: "ok", body: r.body, at: action.at, failure: null }
        // An incident that expired or vanished is not shown from before: its evidence is gone.
        else if (prev && prev.status === "ok" && !GONE.has(r.code)) parts[name] = { ...prev, failure: r.code }
        else parts[name] = { status: "failed", code: r.code, ...(r.expiredAt !== undefined ? { expiredAt: r.expiredAt } : {}) }
      }
      const sameWindow = state.window !== null && state.window.from === o.window.from && state.window.to === o.window.to
      const list = listPart(parts)
      const firstCursor = list?.status === "ok" && list.failure === null ? (list.body as EventsBody).next_cursor : null
      const inc = parts.incidents
      const incCursor = inc?.status === "ok" && inc.failure === null ? (inc.body as IncidentListBody).next_cursor : null
      return {
        ...state, status: "ready", refusal: null, refreshing: false, window: o.window, parts,
        // Older pages belong to one window; a moved window starts paging again.
        older: sameWindow ? state.older : [],
        olderCursor: sameWindow && state.older.length > 0 ? state.olderCursor : firstCursor,
        olderFailure: sameWindow ? state.olderFailure : null,
        incOlder: sameWindow ? state.incOlder : [],
        incCursor: sameWindow && state.incOlder.length > 0 ? state.incCursor : incCursor,
        incFailure: sameWindow ? state.incFailure : null,
      }
    }
    case "older_start":
      return { ...state, olderBusy: true, olderFailure: null }
    case "older_refused":
      return { ...emptyStored(state.key), status: "refused", refusal: action.refusal, refreshing: false }
    case "older_loaded": {
      const r = action.result
      if (!r.ok) return { ...state, olderBusy: false, olderFailure: r.code }
      const page = (r.body as EventsBody)
      return { ...state, olderBusy: false, older: [...state.older, page.events], olderCursor: page.next_cursor }
    }
    case "inc_older_start":
      return { ...state, incBusy: true, incFailure: null }
    case "inc_older_loaded": {
      const r = action.result
      if (!r.ok) return { ...state, incBusy: false, incFailure: r.code }
      const page = r.body as IncidentListBody
      return { ...state, incBusy: false, incOlder: [...state.incOlder, page.incidents], incCursor: page.next_cursor }
    }
  }
}

/** Whether another page may be held: a pinned window, a cursor, and room under the page bound. */
export function mayLoadOlder(state: StoredState, pinned: boolean): "yes" | "live" | "none" | "bound" {
  if (state.olderCursor === null) return "none"
  if (!pinned) return "live"
  return 1 + state.older.length >= HEALTH_UI_LIMITS.eventPages ? "bound" : "yes"
}

/** Whether another incident page may be held: the same rule as events, under its own page bound. */
export function mayLoadOlderIncidents(state: StoredState, pinned: boolean): "yes" | "live" | "none" | "bound" {
  if (state.incCursor === null) return "none"
  if (!pinned) return "live"
  return 1 + state.incOlder.length >= HEALTH_UI_LIMITS.incidentPages ? "bound" : "yes"
}

/** Every incident held, newest opened first, without repeats. */
export function heldIncidents(state: StoredState): IncidentRow[] {
  const first = partBody(state, "incidents")?.incidents ?? []
  const seen = new Set<string>()
  const out: IncidentRow[] = []
  for (const r of [first, ...state.incOlder].flat()) {
    if (seen.has(r.id)) continue
    seen.add(r.id)
    out.push(r)
  }
  return out
}

/** Every event held, newest first, without repeats (a late row can appear on two pages). */
export function heldEvents(state: StoredState): StoredEvent[] {
  const list = listPart(state.parts)
  const first = list?.status === "ok" ? (list.body as EventsBody).events : []
  const seen = new Set<string>()
  const out: StoredEvent[] = []
  for (const e of [first, ...state.older].flat()) {
    if (seen.has(e.id)) continue
    seen.add(e.id)
    out.push(e)
  }
  return out
}

export function partBody<N extends PartName>(state: StoredState, name: N): PartBody[N] | null {
  const p = state.parts[name]
  return p && p.status === "ok" ? (p.body as PartBody[N]) : null
}
