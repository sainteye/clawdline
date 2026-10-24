/* Network wiring for agent investigations: the machines one can run on, and the start.
 *
 * Starting one is an explicit POST with an Idempotency-Key the caller keeps until the outcome is
 * known: an answer that never arrived is retried with the same key and body, never a new key.
 * Nothing is imported at run time except the loader's own helpers, so node's tests load it as is. */
import type { Refusal, StoredFailure } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { errorInfo, failureFor, HEALTH_UI_LIMITS } from "./cloud.ts"
import type { Investigation } from "./investigations.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseInvestigation } from "./investigations.ts"

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => v !== null && typeof v === "object" && !Array.isArray(v)
const REF = /^[A-Za-z0-9_-]{1,128}$/
const ref = (v: unknown): string | null => (typeof v === "string" && REF.test(v) ? v : null)

/* ---- where an investigation can run ----------------------------------------- */

/** `GET /su/v1/health/investigation-destinations`: one row per real machine, as Cloud resolves it. */
export const DESTINATION_SCHEMA = "clawdline.health_investigation.destinations.v1"
export const DESTINATION_STATES = ["ready", "setup_required", "ambiguous", "offline", "unknown"] as const
export type DestinationState = (typeof DESTINATION_STATES)[number]

/** A machine the operator may pick. `hook_id` is non-null only on a `ready` row; every other row is shown, never chosen.
 *  `raw` keeps a state this page does not know, so it is said as unknown rather than dropped. */
export type Destination = { machine_id: string; machine_name: string; state: DestinationState; raw: string | null; hook_id: string | null; reason: string }
export type Destinations = { status: "ok"; complete: boolean; machines: Destination[] } | { status: "unavailable" }
export type DestinationsResult = { ok: true; destinations: Destinations } | { ok: false; code: StoredFailure | "not_supported" } | { refused: Refusal }

const NAME_MAX = 256
const REASON_MAX = 128

/** Null is an answer the page cannot trust: a wrong schema or status, a row without a machine or name, two rows for one
 *  machine, or two machines claiming one hook. Each would make a list look complete when it is not, so all of it fails.
 *  A state this page does not know, or a `ready` row without a hook, stays on screen as unknown and marks the list
 *  incomplete. */
export function parseDestinations(body: unknown): Destinations | null {
  if (!isObj(body) || body.schema !== DESTINATION_SCHEMA || typeof body.complete !== "boolean") return null
  if (body.status === "unavailable") return { status: "unavailable" }
  if (body.status !== "ok" || !Array.isArray(body.machines)) return null
  let complete = body.complete
  const machines: Destination[] = []
  const seenMachine = new Set<string>()
  const seenHook = new Set<string>()
  for (const m of body.machines) {
    if (!isObj(m)) return null
    const id = ref(m.machine_id)
    const name = typeof m.machine_name === "string" ? m.machine_name.trim() : ""
    if (id === null || name === "" || name.length > NAME_MAX || seenMachine.has(id)) return null
    seenMachine.add(id)
    if (m.hook_id !== null && ref(m.hook_id) === null) return null
    const hook = m.hook_id === null ? null : (m.hook_id as string)
    const reason = typeof m.reason === "string" && m.reason.length <= REASON_MAX ? m.reason : ""
    const known = (DESTINATION_STATES as readonly unknown[]).includes(m.status)
    let state: DestinationState = known ? m.status as DestinationState : "unknown"
    let raw = known ? null : typeof m.status === "string" ? m.status.slice(0, 64) : "—"
    if (state === "ready" && hook === null) { state = "unknown"; raw = "ready" }
    if (!known || raw !== null) complete = false
    if (state === "ready") {
      if (seenHook.has(hook as string)) return null
      seenHook.add(hook as string)
    }
    machines.push({ machine_id: id, machine_name: name, state, raw, hook_id: state === "ready" ? hook : null, reason })
  }
  return { status: "ok", complete, machines }
}

/** The start body for the chosen machine: its own `ready` hook, or nothing when it cannot run one. */
export function startRequestFor(d: Destinations, machineId: string, revision: number): StartRequest | null {
  if (d.status !== "ok") return null
  const m = d.machines.find((x) => x.machine_id === machineId)
  return m && m.state === "ready" && m.hook_id !== null ? { hook_id: m.hook_id, expected_incident_revision: revision } : null
}

async function cappedText(res: Response): Promise<string | null> {
  const text = await res.text()
  return new TextEncoder().encode(text).length > HEALTH_UI_LIMITS.bodyBytes ? null : text
}

/** The page is only open to an operator, so a 404 here is a Cloud without the route: said as such, with no
 *  fallback to the webhook list, which cannot tell which hook runs an investigation. */
export async function fetchDestinations(get: typeof fetch, apiOrigin: string, signal: AbortSignal): Promise<DestinationsResult> {
  let res: Response
  try {
    res = await get(`${apiOrigin}/su/v1/health/investigation-destinations`, { credentials: "include", cache: "no-store", headers: { accept: "application/json" }, signal })
  } catch {
    return { ok: false, code: "network" }
  }
  const text = await cappedText(res).catch(() => null)
  if (res.status === 401) return { refused: "signed_out" }
  if (res.status === 404) return { ok: false, code: "not_supported" }
  if (res.status === 403) return { refused: "forbidden" }
  if (!res.ok) return { ok: false, code: res.status === 429 ? "rate_limited" : res.status >= 500 ? "unavailable" : "http_error" }
  if (text === null) return { ok: false, code: "body_too_large" }
  try {
    const parsed = parseDestinations(JSON.parse(text))
    return parsed === null ? { ok: false, code: "contract_shape" } : { ok: true, destinations: parsed }
  } catch {
    return { ok: false, code: "contract_shape" }
  }
}

/* ---- starting one --------------------------------------------------------- */

/** The Cloud's stable refusal codes for a start (contract §1), kept as sent so each gets its own words. */
export const START_REFUSALS = [
  "incident_not_found", "incident_expired", "incident_revision_moved", "investigation_open", "investigation_limit",
  "hook_unavailable", "pro_required", "webhook_rate_limited", "webhook_queue_full", "idempotency_conflict",
  "health_store_unavailable", "health_busy", "bad_origin",
] as const
export type StartRefusal = (typeof START_REFUSALS)[number]

export type StartRequest = { hook_id: string; expected_incident_revision: number }
export type StartResult =
  | { ok: true; investigation: Investigation | null; replayed: boolean }
  /** Cloud gave a definite answer: the key is spent and a new attempt takes a new one. */
  | { ok: false; definite: true; code: StartRefusal | StoredFailure }
  /** No definite answer reached the page: retry with the same key and body. */
  | { ok: false; definite: false; code: "network" | "unavailable" | "contract_shape" }
  | { refused: Refusal }

/** A fresh key for one start; the caller keeps it until a definite result. */
export function newIdempotencyKey(random: () => string = () => globalThis.crypto.randomUUID()): string {
  return `inv-${random()}`
}

export async function startInvestigation(get: typeof fetch, apiOrigin: string, incidentId: string, account: string | null, req: StartRequest, key: string, signal: AbortSignal): Promise<StartResult> {
  const q = account !== null ? `?${new URLSearchParams({ account }).toString()}` : ""
  let res: Response
  try {
    res = await get(`${apiOrigin}/su/v1/health/incidents/${encodeURIComponent(incidentId)}/investigations${q}`, {
      method: "POST", credentials: "include", cache: "no-store", signal,
      headers: { accept: "application/json", "content-type": "application/json", "idempotency-key": key },
      body: JSON.stringify({ hook_id: req.hook_id, expected_incident_revision: req.expected_incident_revision }),
    })
  } catch {
    return { ok: false, definite: false, code: "network" }
  }
  const text = await cappedText(res).catch(() => null)
  if (res.status === 202) {
    try {
      const body = JSON.parse(text ?? "") as Obj
      if (body.schema !== "clawdline.health_investigation.started.v1" || typeof body.replayed !== "boolean") return { ok: false, definite: false, code: "contract_shape" }
      return { ok: true, investigation: parseInvestigation(body.investigation), replayed: body.replayed }
    } catch {
      return { ok: false, definite: false, code: "contract_shape" }
    }
  }
  const code = errorInfo(text).code
  if ((START_REFUSALS as readonly string[]).includes(code ?? "")) {
    // Overloaded or unmigrated storage committed nothing: Cloud says so, so a retry may use a new key.
    return { ok: false, definite: true, code: code as StartRefusal }
  }
  if (res.status === 403) return { ok: false, definite: true, code: "http_error" }
  const failure = failureFor(res.status, code)
  if (failure === "signed_out" || failure === "forbidden") return { refused: failure }
  // A 5xx without a typed code may have committed: the outcome is unknown, so the key is kept.
  if (res.status >= 500) return { ok: false, definite: false, code: "unavailable" }
  return { ok: false, definite: true, code: failure }
}
