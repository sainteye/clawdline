export interface NativePairingReply {
  ok: boolean
  error?: string
  machine_id?: string
  machine_name?: string
  native_setup?: boolean
  configured?: boolean
  enabled?: boolean
  state?: string
  fingerprint?: string
  connection?: { phase: string; user_code?: string; verification_url?: string; expires_at?: number; next_poll_at?: number; code?: string }
  pairing_invitation?: boolean
  pairing?: { phase: string; link?: string; invitation_id?: string; expires_at?: number; machine_fingerprint?: string; viewer_fingerprint?: string; viewer_device_id?: string; error?: string }
}

type NativeHost = { webkit?: { messageHandlers?: { clawdlinePairLocal?: { postMessage(body: unknown): Promise<unknown> } } } }

export function nativePairingAvailable(host: NativeHost = globalThis as NativeHost): boolean {
  return typeof host.webkit?.messageHandlers?.clawdlinePairLocal?.postMessage === "function"
}

/** This bridge uses the Mac's local credential; Cloud never gains a pairing write route. */
export async function nativePairing(body: { kind: "status" | "invitation" | "invitation-status" | "connect" | "connection-status" | "connection-cancel"; machine_id?: string },
  host: NativeHost = globalThis as NativeHost): Promise<NativePairingReply> {
  const handler = host.webkit?.messageHandlers?.clawdlinePairLocal
  if (!handler) throw Object.assign(new Error("unavailable"), { code: "unavailable" })
  const reply = await handler.postMessage(body) as NativePairingReply
  if (!reply?.ok) throw Object.assign(new Error(reply?.error || "pairing_failed"), { code: reply?.error || "pairing_failed" })
  return reply
}
