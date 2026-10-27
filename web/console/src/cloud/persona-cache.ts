// A single tab-scoped display catalog survives reloads. It is never shown
// until the authorized machine confirms its exact digest on a fresh read.
export const PERSONA_DISPLAY_CACHE_KEY = "clawdline.personas.display-v1"
const MAX_CATALOG_BYTES = 8 * 1024 * 1024 // cloud.read_plaintext_bytes
const VERSION = /^[a-f0-9]{64}$/

let cacheGeneration = 0

interface DisplayReply { view?: string; version?: string; catalog?: unknown; unchanged?: boolean }
interface Held { scope: string; version: string; text: string }
interface StorageLike { getItem(key: string): string | null; setItem(key: string, value: string): void; removeItem(key: string): void }

function storage(): StorageLike | null {
  try { return typeof sessionStorage === "undefined" ? null : sessionStorage }
  catch { return null }
}

export function clearPersonaDisplayCache(store: StorageLike | null = storage()): void {
  cacheGeneration++
  try { store?.removeItem(PERSONA_DISPLAY_CACHE_KEY) }
  catch { /* refusal-ok: disabled browser storage does not prevent a fresh read. */ }
}

async function digest(text: string): Promise<string | null> {
  if (typeof crypto === "undefined" || typeof crypto.subtle?.digest !== "function") return null
  const bytes = new TextEncoder().encode(text)
  if (bytes.length > MAX_CATALOG_BYTES) return null
  return [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))]
    .map((byte) => byte.toString(16).padStart(2, "0")).join("")
}

// Object keys in this display schema are ASCII identifiers. The string
// escaping matches encoding/json so HTML and Unicode separators hash equally.
export function canonicalPersonaCatalog(catalog: unknown): string {
  return JSON.stringify(catalog, (_key, value: unknown) => value && typeof value === "object" && !Array.isArray(value)
    ? Object.fromEntries(Object.keys(value).sort().map((key) => [key, (value as Record<string, unknown>)[key]])) : value)
    .replace(/[<>&\u2028\u2029]/g, (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"))
}

export function personaDisplayCatalog(reply: unknown): unknown {
  const body = reply as DisplayReply | null
  return body?.view === "display-v1" && body.catalog ? body.catalog : reply
}

/** The single slot includes account, viewer device and target machine identity. */
export async function readPersonaDisplay(
  ask: (fields: Record<string, unknown>) => Promise<Response>,
  scope: string | null,
  signal?: AbortSignal | null,
  store: StorageLike | null = storage(),
): Promise<Response> {
  let held: Held | null = null
  if (scope && store) {
    try {
      const raw = store.getItem(PERSONA_DISPLAY_CACHE_KEY)
      if (raw && raw.length <= MAX_CATALOG_BYTES && new TextEncoder().encode(raw).length <= MAX_CATALOG_BYTES) {
        const value = JSON.parse(raw) as Held
        if (value.scope === scope && VERSION.test(value.version) && typeof value.text === "string" &&
          await digest(value.text) === value.version) held = value
      }
    } catch { /* refusal-ok: a corrupt cache is discarded before it can be shown. */ }
    if (!held) clearPersonaDisplayCache(store)
  }
  if (!scope) clearPersonaDisplayCache(store)
  const started = cacheGeneration
  if (signal?.aborted) throw signal.reason ?? new Error("Aborted")
  let response: Response
  try { response = await ask({ view: "display-v1", ...(held ? { version: held.version } : {}) }) }
  catch (error) { clearPersonaDisplayCache(store); throw error }
  if (!response.ok) { clearPersonaDisplayCache(store); return response }
  let body: DisplayReply
  try { body = await response.json() as DisplayReply }
  catch { clearPersonaDisplayCache(store); return Response.json({ error: "persona_catalog_invalid" }, { status: 502 }) }
  if (signal?.aborted) throw signal.reason ?? new Error("Aborted")
  // A stale capability may still reach an old daemon that ignores the view.
  // Its ordinary catalog is usable, but never admitted to this version cache.
  if (body.view !== "display-v1") { clearPersonaDisplayCache(store); return Response.json(body) }
  let text: string
  if (body.unchanged === true && held && body.version === held.version) text = held.text
  else if (body.catalog && VERSION.test(body.version ?? "") &&
    await digest(canonicalPersonaCatalog(body.catalog)) === body.version) text = canonicalPersonaCatalog(body.catalog)
  else {
    clearPersonaDisplayCache(store)
    return Response.json({ error: "persona_catalog_invalid" }, { status: 502 })
  }
  let catalog: unknown
  try { catalog = JSON.parse(text) }
  catch { clearPersonaDisplayCache(store); return Response.json({ error: "persona_catalog_invalid" }, { status: 502 }) }
  if (signal?.aborted) throw signal.reason ?? new Error("Aborted")
  if (scope && store && started === cacheGeneration) {
    try {
      const saved = JSON.stringify({ scope, version: body.version, text })
      if (saved.length <= MAX_CATALOG_BYTES && new TextEncoder().encode(saved).length <= MAX_CATALOG_BYTES) store.setItem(PERSONA_DISPLAY_CACHE_KEY, saved)
      else clearPersonaDisplayCache(store)
    } catch { /* refusal-ok: storage quota failure only forfeits the next reload's saving. */ }
  }
  return Response.json(catalog)
}
