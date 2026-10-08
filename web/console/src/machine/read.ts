import { catalogRefusalDetail, catalogWord } from "../catalog.js"
import type { MachineUsage } from "@clawdline/contract"
import { RefusalError, TransportError, asMachineNeedsUpdate, isRefusal } from "@clawdline/core"
import { nextWord } from "../next-strings.js"
import { client } from "../client.js"

/**
 * `/v1/machine/usage`, including disk and grouped reclaim readings, asked
 * the way `pages/settings/capacity.ts` asks
 * `/v1/capacity`: through `client.url` and the page's `fetch`, which a console
 * reading a machine through Clawdline Cloud answers from the relay as the
 * `machine-usage` word. The read is spelled beside its method on one line
 * because `cloud/carry.test.ts` reads it there.
 *
 * The route waits half a second on a first reading (a CPU share needs two), so
 * the deadline is longer than that on a machine that is swapping — which is
 * exactly when this is opened.
 */
export function readMachineUsage(): Promise<MachineUsage> {
  return call("GET", "/v1/machine/usage")
}

async function call(method: string, path: string): Promise<MachineUsage> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 20_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { method, credentials: "same-origin", signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${method} ${path} did not complete`, cause)
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
  return parsed as MachineUsage
}

/** A failure as the dashboard says it. */
export function failureWords(err: unknown, zh: boolean): string {
  // A machine older than the usage route: the console's one needs-update
  // sentence (machine/NeedsUpdate.tsx), not a failure.
  if (asMachineNeedsUpdate(err)) return nextWord("machineNeedsUpdate")
  if (err instanceof RefusalError) {
    if (err.code === "machine_usage_unsupported") {
      return catalogWord("literal", "ed6c1de76c4f")
    }
    if (err.code === "cloud_not_carried") return nextWord("cloudNotCarried")
    if (err.code === "unknown_command") return catalogWord("literal", "fabc8bde5489")
    return catalogRefusalDetail(err)?.text || err.detail || err.code
  }
  if (err instanceof TransportError) {
    return catalogWord("literal", "eab8191e445c")
  }
  return err instanceof Error ? err.message : String(err)
}

/** Mark a producer sentence with its actual language for screen readers. */
export function failureWordsLanguage(err: unknown): string | undefined {
  if (!(err instanceof RefusalError)) return undefined
  if (err.code === "machine_usage_unsupported" || err.code === "cloud_not_carried" || err.code === "unknown_command") return undefined
  return catalogRefusalDetail(err)?.lang
}
