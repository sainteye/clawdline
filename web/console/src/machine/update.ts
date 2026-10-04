import type { UpdateStatus } from "@clawdline/contract"
import { client } from "../client.js"
import { settleUpdateRead } from "./update-model.js"

/**
 * `/v1/update`, asked the way `read.ts` asks `/v1/machine/usage`: through
 * `client.url` and the page's `fetch`, which a console reading a machine
 * through Clawdline Cloud answers from the relay as the `update` word. The
 * read is spelled beside its method on one line because `cloud/carry.test.ts`
 * reads it there.
 *
 * It never throws: a refusal, a missing route or a machine that did not answer
 * is null, and the notice stays silent.
 */
export function readUpdateStatus(): Promise<UpdateStatus | null> {
  return call("GET", "/v1/update")
}

async function call(method: string, path: string): Promise<UpdateStatus | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  try {
    const res = await fetch(client.url(path), { method, credentials: "same-origin", signal: controller.signal })
    const text = await res.text()
    let parsed: unknown = null
    try {
      parsed = text ? JSON.parse(text) : null
    } catch {
      return null
    }
    return settleUpdateRead(res.ok, parsed)
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}
