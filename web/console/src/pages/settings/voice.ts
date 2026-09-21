import type { VoiceLanguage } from "@clawdline/contract"
import { client } from "../../client.js"

/**
 * `/v1/voice/language`, what this daemon will read the next recording as and
 * who decided it (api/v1/voice.schema.json).
 *
 * The decision is the daemon's, made from its own settings file and its own
 * machine, because that is where whisper runs; this page only says it back.
 * A failure is null rather than a throw, as `readTunnelStatus` answers one: the
 * picker still works without the sentence under it.
 */
export async function readVoiceLanguage(): Promise<VoiceLanguage | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  try {
    const res = await fetch(client.url("/v1/voice/language"), { signal: controller.signal, cache: "no-store" })
    if (!res.ok) return null
    return (await res.json()) as VoiceLanguage
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}
