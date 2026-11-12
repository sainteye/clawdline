import * as L from "../legacy/bridge.js"

/**
 * One `/v1/browser/*` request for the Chrome panel and the browser setup
 * guide. Through Clawdline Cloud the page's fetch is the relay's, so the same
 * path reaches the machine as its chrome-* word. A refusal keeps its code, so
 * the caller can name it (refusals/scan.ts).
 */
export async function browserCall<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers: body === undefined ? undefined : { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new Error(L.strings.webOffline)
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    // The daemon's refusal envelope, kept whole so the formatter can name it.
    const raw = data?.error
    const code = typeof raw === "string" ? raw : typeof raw?.code === "string" ? raw.code : "http_" + res.status
    const refusal = new Error(typeof data?.detail === "string" ? data.detail : typeof raw?.message === "string" ? raw.message : code) as Error & { code: string }
    refusal.name = "RefusalError"
    refusal.code = code
    throw refusal
  }
  return data as T
}
