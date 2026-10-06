import type { MachineUsage } from "@clawdline/contract"
import { RefusalError, TransportError, asMachineNeedsUpdate, isRefusal } from "@clawdline/core"
import { nextWord } from "../next-strings.js"
import { client } from "../client.js"

/**
 * `/v1/machine/usage`, asked the way `pages/settings/capacity.ts` asks
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
      return zh ? "這台機器的作業系統還沒有用量讀取器（目前支援 Linux 和 macOS）。" : "This machine's system has no usage reader yet (Linux and macOS for now)."
    }
    // This console not carrying the read over Cloud is the console's fact,
    // and updating the machine would not change it.
    if (err.code === "cloud_not_carried") return nextWord("cloudNotCarried")
    return err.detail || err.code
  }
  if (err instanceof TransportError) {
    return zh ? "機器沒有回應，可能正忙著；會自動再試。" : "The machine did not answer — it may be busy. Trying again."
  }
  return err instanceof Error ? err.message : String(err)
}
