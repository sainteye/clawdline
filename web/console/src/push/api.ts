import type { PushKey, PushOK, PushSent, PushSubscribed } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../client.js"

/**
 * `/v1/push/*` (`net/live.js`'s four push calls).
 *
 * Asked here rather than through the shared client because these routes are
 * this feature's and nobody else's. The subscription goes up **exactly as the
 * browser wrote it** — it is the browser's own object, endpoint and keys, and
 * anything this end reshaped would be a chance to get a credential wrong on the
 * way past.
 *
 * These routes answer a refusal in the gate's envelope — `{"error":{"code",
 * "message"}}`, which is what the Swift app's page reads — rather than the
 * `{error, detail}` shape the rest of this daemon uses. Both are read below and
 * both become a `RefusalError`, so `failureSentence` and `e.code` work the same
 * either way. A screen that had to know which envelope it was holding would get
 * it wrong the first time a route moved.
 */
async function call<T>(path: string, init: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 20_000)
  let res: Response
  try {
    res = await fetch(client.url(path), {
      credentials: "same-origin",
      ...init,
      signal: controller.signal,
    })
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    const nested = authRefusal(parsed)
    if (nested) throw new RefusalError(res.status, nested, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as T
}

/** `{"error":{"code","message","request_id"}}`, read as the one refusal shape. */
function authRefusal(value: unknown): { error: string; detail: string } | null {
  const held = (value as { error?: { code?: unknown; message?: unknown } } | null)?.error
  if (!held || typeof held !== "object" || typeof held.code !== "string") return null
  return { error: held.code, detail: typeof held.message === "string" ? held.message : "" }
}

/** The application server key. Asking for it is what mints one on the daemon. */
export function pushKey(): Promise<PushKey> {
  return call<PushKey>("/v1/push/key", { method: "GET" })
}

export function pushSubscribe(subscription: unknown): Promise<PushSubscribed> {
  return call<PushSubscribed>("/v1/push/subscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(subscription),
  })
}

export function pushUnsubscribe(id: string): Promise<PushOK> {
  return call<PushOK>("/v1/push/unsubscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  })
}

/**
 * Reaches this device and nothing else — the daemon sends only to the
 * subscriptions the asking device owns, so pressing this on a phone buzzes that
 * phone and nobody else's.
 *
 * `sessionId` is optional, and it is what turns a light into a loop. Without it
 * the button answers half a question — *did a notification arrive* — and the
 * half that actually goes wrong is the other one: *does tapping one get me back
 * to my session*. Given the session the reader has open, the notification that
 * arrives carries that session's address, so the whole road can be walked on
 * purpose instead of waited for. The daemon checks the id against the sessions
 * it is watching and falls back to the list, so a stale one is safe to send.
 */
export function pushTest(sessionId: string | null): Promise<PushSent> {
  return call<PushSent>("/v1/push/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(sessionId ? { session_id: sessionId } : {}),
  })
}
