import type { SettingsRequest, SettingsSnapshot } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"

/**
 * `/v1/settings`, this app's own config.json.
 *
 * Asked here rather than through the shared client because the route is this
 * page's and nobody else's; the request and its failures are the client's own
 * shape all the same, so `failureSentence` reads them the way it reads any
 * other refusal.
 */
async function call(init: RequestInit): Promise<SettingsSnapshot> {
  const path = "/v1/settings"
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { ...init, signal: controller.signal })
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
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as SettingsSnapshot
}

export function readSettings(): Promise<SettingsSnapshot> {
  return call({ method: "GET" })
}

/** Change the keys given; a key left null is left as the file has it. */
export function writeSettings(change: Partial<SettingsRequest>): Promise<SettingsSnapshot> {
  const body: SettingsRequest = { hotkey: change.hotkey ?? null, scope_app: change.scope_app ?? null }
  return call({
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}
