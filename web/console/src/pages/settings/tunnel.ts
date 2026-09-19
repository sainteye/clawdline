import type { TunnelStatus } from "@clawdline/contract"
import { client } from "../../client.js"

/**
 * `/v1/tunnel`, what this daemon's cloudflared is doing (api/v1/tunnel.schema.json).
 *
 * Behind **this machine's own token**, like the Cloud line's status and for a
 * sharper reason: while a quick tunnel is up, its address is the access. So a
 * paired phone reading this window gets a refusal, and the card says it could
 * not read rather than drawing a tunnel that is off.
 *
 * A refusal is null rather than a throw, as `readCloudStatus` answers it.
 */
export async function readTunnelStatus(): Promise<TunnelStatus | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  try {
    const res = await fetch(client.url("/v1/tunnel"), { signal: controller.signal, cache: "no-store" })
    if (!res.ok) return null
    return (await res.json()) as TunnelStatus
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}
