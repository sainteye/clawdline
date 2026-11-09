/* The Cloud's audited incident export and the operator's acknowledgement (private contract §11.4).
 *
 * The export is the Cloud's own artifact: the exact bytes it audited before sending, never a copy
 * this page assembles. It is read under a 1 MiB bound (the Cloud refuses to export more), and its
 * `content_hash` is recomputed here over the key-sorted JSON of `bundle`, the Cloud's own basis
 * (`canonicalJson` in its domain module), with the browser's WebCrypto. A hash of the pretty-printed
 * or received text would prove nothing about the bundle, so it is never used. Bytes that fail any
 * check are dropped and never offered for download or copy.
 *
 * Acknowledgement is a human act: an explicit POST from a button, never a side effect of a read,
 * and it closes the incident by review. It never says the incident recovered.
 * Nothing is imported at run time except the loader's own helpers, so node's tests load it as is. */
import type { Refusal, StoredFailure } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { errorInfo, failureFor, HEALTH_UI_LIMITS, healthGet, HealthReadBusy } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { CONTENT_HASH, INCIDENT_ID, parseBundle, StoredContractError } from "./stored.ts"

/** The Cloud's canonical form: keys sorted at every level, no spaces (its `canonicalJson`). */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJson((value as Record<string, unknown>)[key])}`).join(",")}}`
  }
  return JSON.stringify(value)
}

export async function sha256Hex(text: string, subtle: SubtleCrypto = globalThis.crypto.subtle): Promise<string> {
  const digest = await subtle.digest("SHA-256", new TextEncoder().encode(text))
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("")
}

/** Reads a body up to `max` bytes and stops reading past it, so an oversized answer is never held. */
export async function readCapped(res: Response, max: number): Promise<Uint8Array | null> {
  const declared = Number(res.headers.get("content-length"))
  if (res.headers.has("content-length") && Number.isFinite(declared) && declared > max) {
    await res.body?.cancel().catch(() => undefined)
    return null
  }
  if (!res.body) {
    const buf = new Uint8Array(await res.arrayBuffer())
    return buf.byteLength > max ? null : buf
  }
  const reader = res.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    total += value.byteLength
    if (total > max) {
      await reader.cancel().catch(() => undefined)
      return null
    }
    chunks.push(value)
  }
  const out = new Uint8Array(total)
  let at = 0
  for (const c of chunks) { out.set(c, at); at += c.byteLength }
  return out
}

export type ExportFailure = StoredFailure | "export_too_large" | "hash_mismatch" | "wrong_incident" | "not_json"

export type VerifiedExport = {
  /** The bytes as the Cloud sent them; this is what is downloaded or copied. */
  text: string
  bytes: number
  filename: string
  incidentId: string
  revision: number
  latestRevision: number
  contentHash: string
  exportedAt: string
  expiresAt: string
}

export type ExportResult =
  | { ok: true; value: VerifiedExport }
  | { ok: false; code: ExportFailure; expiredAt?: string | null }
  | { refused: Refusal }

const ISO = (v: unknown): v is string => typeof v === "string" && v.length <= 40 && Number.isFinite(Date.parse(v))

/** Checks an export body against the incident and revision asked for, and its bundle against its hash. */
export async function verifyExport(text: string, incidentId: string, revision: number | null, subtle?: SubtleCrypto): Promise<{ ok: true; value: VerifiedExport } | { ok: false; code: ExportFailure }> {
  let json: Record<string, unknown>
  try {
    const parsed: unknown = JSON.parse(text)
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return { ok: false, code: "not_json" }
    json = parsed as Record<string, unknown>
  } catch {
    return { ok: false, code: "not_json" }
  }
  // Only the two documented keys: anything else in the file would be copied out unread.
  if (Object.keys(json).sort().join(",") !== "bundle,export,schema_version") return { ok: false, code: "contract_shape" }
  if (json.schema_version !== 1) return { ok: false, code: "contract_schema" }
  const ex = json.export as Record<string, unknown> | null
  if (ex === null || typeof ex !== "object" || Array.isArray(ex)) return { ok: false, code: "contract_shape" }
  const n = ex.revision
  const latest = ex.latest_revision
  const hash = ex.content_hash
  if (typeof ex.incident_id !== "string" || !INCIDENT_ID.test(ex.incident_id) || typeof hash !== "string" || !CONTENT_HASH.test(hash)
    || !Number.isSafeInteger(n) || !Number.isSafeInteger(latest) || (n as number) < 1 || (n as number) > (latest as number)
    || !ISO(ex.exported_at) || !ISO(ex.expires_at) || ex.hash_basis !== "sha256 over key-sorted JSON of `bundle`") {
    return { ok: false, code: "contract_shape" }
  }
  if (ex.incident_id !== incidentId || (revision !== null && n !== revision)) return { ok: false, code: "wrong_incident" }
  try {
    const bundle = parseBundle(json.bundle)
    if (bundle.manifest.incident_id !== incidentId || bundle.manifest.revision !== n) return { ok: false, code: "wrong_incident" }
  } catch (err) {
    if (err instanceof StoredContractError) return { ok: false, code: err.code }
    throw err
  }
  const computed = `sha256:${await sha256Hex(canonicalJson(json.bundle), subtle)}`
  if (computed !== hash) return { ok: false, code: "hash_mismatch" }
  return {
    ok: true,
    value: {
      text, bytes: new TextEncoder().encode(text).byteLength, filename: `${incidentId}.r${n as number}.json`, incidentId,
      revision: n as number, latestRevision: latest as number, contentHash: hash, exportedAt: ex.exported_at as string, expiresAt: ex.expires_at as string,
    },
  }
}

function scoped(apiOrigin: string, path: string, params: Record<string, string>): string {
  const q = new URLSearchParams(params).toString()
  return `${apiOrigin}/su/v1/health/${path}${q ? `?${q}` : ""}`
}

/** GET the export of one revision (the latest when null). Nothing is kept unless every check passes. */
export async function fetchIncidentExport(get: typeof fetch, apiOrigin: string, incidentId: string, revision: number | null, account: string | null, signal: AbortSignal, subtle?: SubtleCrypto): Promise<ExportResult> {
  if (!INCIDENT_ID.test(incidentId)) return { ok: false, code: "incident_not_found" }
  const params: Record<string, string> = {}
  if (revision !== null) params.revision = String(revision)
  if (account !== null) params.account = account
  let res: Response
  try {
    res = await healthGet(get, scoped(apiOrigin, `incidents/${encodeURIComponent(incidentId)}/export`, params), { credentials: "include", headers: { accept: "application/json" }, signal })
  } catch (error) {
    return { ok: false, code: error instanceof HealthReadBusy ? "rate_limited" : "network" }
  }
  if (!res.ok) {
    const raw = await readCapped(res, 16 * 1024).catch(() => null)
    const info = errorInfo(raw === null ? null : new TextDecoder().decode(raw))
    const failure = failureFor(res.status, info.code)
    if (failure === "signed_out" || failure === "forbidden") return { refused: failure }
    return failure === "incident_expired" ? { ok: false, code: failure, expiredAt: info.expiredAt } : { ok: false, code: failure }
  }
  let raw: Uint8Array | null
  try {
    raw = await readCapped(res, HEALTH_UI_LIMITS.incidentExportBytes)
  } catch {
    return { ok: false, code: "network" }
  }
  if (raw === null) return { ok: false, code: "export_too_large" }
  let text: string
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(raw)
  } catch {
    return { ok: false, code: "not_json" }
  }
  return verifyExport(text, incidentId, revision, subtle)
}

export type AckResult = { ok: true; revised: boolean } | { ok: false; code: StoredFailure } | { refused: Refusal }

/** The operator's acknowledgement. The browser adds the console's own `Origin`; the Cloud refuses
 *  any other (`403 bad_origin`), and nothing here can forge it. */
export async function acknowledgeIncident(get: typeof fetch, apiOrigin: string, incidentId: string, account: string | null, signal: AbortSignal): Promise<AckResult> {
  if (!INCIDENT_ID.test(incidentId)) return { ok: false, code: "incident_not_found" }
  let res: Response
  try {
    res = await get(scoped(apiOrigin, `incidents/${encodeURIComponent(incidentId)}/acknowledge`, account !== null ? { account } : {}), { method: "POST", credentials: "include", headers: { accept: "application/json" }, signal })
  } catch {
    return { ok: false, code: "network" }
  }
  const raw = await readCapped(res, HEALTH_UI_LIMITS.bodyBytes).catch(() => null)
  const text = raw === null ? null : new TextDecoder().decode(raw)
  if (!res.ok) {
    // 403 from this route is the Origin check, not a refused session: the operator check runs first.
    if (res.status === 403) return { ok: false, code: errorInfo(text).code === "bad_origin" ? "bad_origin" : "http_error" }
    const failure = failureFor(res.status, errorInfo(text).code)
    return failure === "signed_out" || failure === "forbidden" ? { refused: failure } : { ok: false, code: failure }
  }
  try {
    const body = JSON.parse(text ?? "") as { revised?: unknown }
    return typeof body.revised === "boolean" ? { ok: true, revised: body.revised } : { ok: false, code: "contract_shape" }
  } catch {
    return { ok: false, code: "contract_shape" }
  }
}
