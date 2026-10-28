export const SUPPORT_UI_LIMITS = { requestMs: 15_000, refreshMs: 30_000, titleChars: 160, imageCount: 5, imageRawBytes: 10_485_760, imageBytes: 1_048_576, imageEdge: 2048, imageRequestMs: 60_000 } as const
export class SupportFailure extends Error {
  readonly code: string
  readonly definite: boolean
  constructor(code: string, definite: boolean) { super(code); this.code = code; this.definite = definite }
}
export function requestKey(): string { return crypto.randomUUID() }
/** One request per action. An ambiguous outcome keeps the caller's key and body intact. */
export async function supportCall<T>(origin: string, path: string, method = "GET", body?: unknown, key?: string, get: typeof fetch = fetch): Promise<T> {
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), (path === "/v1/support/images" || path === "/v1/community/images") ? SUPPORT_UI_LIMITS.imageRequestMs : SUPPORT_UI_LIMITS.requestMs)
  try {
    const r = await get(origin + path, { method, credentials: "include", cache: "no-store", signal: controller.signal,
      headers: { ...(body === undefined ? {} : { "Content-Type": "application/json" }), ...(key ? { "Idempotency-Key": key } : {}) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }) })
    let value: unknown
    try { value = await r.json() } catch { throw new SupportFailure("support_response_unreadable", false) }
    if (!r.ok) {
      const code = value && typeof value === "object" && "error" in value ? (value.error as { code?: unknown })?.code : null
      throw new SupportFailure(typeof code === "string" ? code : "support_unavailable", r.status >= 400 && r.status < 500)
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new SupportFailure("support_response_unreadable", false)
    const row = value as Record<string, unknown>
    const question = (q: unknown) => {
      if (!q || typeof q !== "object") return false
      const r = q as Record<string, unknown>
      return typeof r.id === "string" && typeof r.title === "string" && typeof r.state === "string" && Number.isSafeInteger(r.revision) && typeof r.reply_locale === "string" &&
        (r.reporter === undefined || !!r.reporter && typeof r.reporter === "object" && typeof (r.reporter as {id?:unknown}).id === "string" && ((r.reporter as {name?:unknown}).name === null || typeof (r.reporter as {name?:unknown}).name === "string") && ((r.reporter as {avatar_url?:unknown}).avatar_url === undefined || (r.reporter as {avatar_url?:unknown}).avatar_url === null || typeof (r.reporter as {avatar_url?:unknown}).avatar_url === "string")) &&
        (r.messages === undefined || Array.isArray(r.messages) && r.messages.every(m => m && typeof m.body === "string" && Array.isArray(m.sources)))
    }
    if (("question" in row && !question(row.question)) || ("questions" in row && (!Array.isArray(row.questions) || !row.questions.every(question))) ||
      ("items" in row && (!Array.isArray(row.items) || row.items.some(r => !r || typeof r.title !== "string" || typeof r.body !== "string" || !Array.isArray(r.sources)))) ||
      ("articles" in row && !Array.isArray(row.articles))) throw new SupportFailure("support_response_unreadable", false)
    const requestURL = new URL(path, "https://schema.invalid")
    if (requestURL.pathname === "/v1/faq" && (typeof row.locale !== "string" || row.locale !== (requestURL.searchParams.get("locale") ?? "en") || !Array.isArray(row.items) ||
      row.items.some(r => r.locale !== row.locale))) throw new SupportFailure("support_response_unreadable", false)
    return value as T
  } catch (e) { if (e instanceof SupportFailure) throw e; throw new SupportFailure("support_outcome_unknown", false) }
  finally { clearTimeout(timeout) }
}
