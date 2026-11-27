import { nextWord } from "../next-strings.js"
import { instaBoxWakeReasonWord } from "./instabox-wake-reason.js"

export const instaBoxProviderPath = "/v1/cloud-providers/instabox"
export const instaBoxWakePath = (machine: string) => `/v1/machines/${encodeURIComponent(machine)}/instabox-wake`

export class InstaBoxRequestError extends Error {
  constructor(message: string, readonly status: number) { super(message) }
}

export async function instaBoxRequest<T>(origin: string, path: string, method = "GET", body?: object, signal?: AbortSignal): Promise<T> {
  const response = await fetch(new URL(path, origin), {
    method, credentials: "include", cache: "no-store", signal,
    ...(body ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
  })
  const result = await response.json().catch(() => null)
  if (!response.ok || !result) {
    const code = typeof result?.error?.code === "string" ? result.error.code : "instabox_unavailable"
    throw new InstaBoxRequestError(code, response.status)
  }
  return result
}

export function instaBoxWakeReason(error: unknown) {
  return nextWord(instaBoxWakeReasonWord(error instanceof Error ? error.message : ""))
}

export type InstaBoxBinding = { schedule_clock_ready?: boolean; configured: boolean; credential_ready: boolean; schedule_webhook_wake_enabled?: boolean; schedule_webhook_wake_status?: string | null }

export function readInstaBoxBinding(origin: string, machine: string, signal?: AbortSignal) {
  return instaBoxRequest<InstaBoxBinding>(origin, instaBoxWakePath(machine), "GET", undefined, signal)
}

export async function wakeInstaBox(origin: string, machine: string, signal?: AbortSignal) {
  try {
    return await instaBoxRequest<unknown>(origin, `${instaBoxWakePath(machine)}/wake`, "POST", undefined, signal)
  } catch (error) {
    if (signal?.aborted || error instanceof InstaBoxRequestError && error.message !== "instabox_unavailable") throw error
    throw new Error("wake_unconfirmed")
  }
}

/** Only an explicitly bound Cloud machine may issue a provider wake request. */
export async function wakeBeforeAssistedPairing(origin: string, machine: string, signal: AbortSignal, onWake: () => void) {
  const binding = await readInstaBoxBinding(origin, machine, signal)
  if (typeof binding.configured !== "boolean" || typeof binding.credential_ready !== "boolean") {
    throw new Error("instabox_unavailable")
  }
  if (!binding.configured) return
  if (!binding.credential_ready) throw new Error("wake_key_missing")
  signal.throwIfAborted()
  onWake()
  await wakeInstaBox(origin, machine, signal)
}
