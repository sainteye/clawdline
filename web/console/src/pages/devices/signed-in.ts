import type { DeviceList } from "@clawdline/contract"
import { client } from "../../client.js"

/**
 * `/v1/auth/devices`: who is signed in through this machine's own door — the
 * browsers `clawdline open` made, the devices that paired with a code, and the
 * ones that used the password (api/v1/auth.schema.json `DeviceList`).
 *
 * Only this machine's own token may read it or revoke from it. The app window
 * carries that token, so it gets the list. A browser signed in as a device of
 * its own is refused with 403, and that refusal is itself the answer to "which
 * one am I": this page is a device, and the only key it can take away is its
 * own, through `/v1/auth/logout`.
 */
export type SignedIn = { kind: "list"; list: DeviceList } | { kind: "device" } | { kind: "failed"; why: string }

async function refusal(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { message?: unknown } }
    if (typeof body?.error?.message === "string" && body.error.message) return body.error.message
  } catch {
    // refusal-ok: a body that is not the envelope falls back to the status.
  }
  return res.status + " " + res.statusText
}

export async function readSignedIn(read: typeof fetch = fetch): Promise<SignedIn> {
  try {
    const res = await read(client.url("/v1/auth/devices"), { cache: "no-store" })
    if (res.status === 403) return { kind: "device" }
    if (!res.ok) return { kind: "failed", why: await refusal(res) }
    return { kind: "list", list: (await res.json()) as DeviceList }
  } catch (error) {
    return { kind: "failed", why: error instanceof Error ? error.message : String(error) }
  }
}

async function post(path: string, read: typeof fetch): Promise<void> {
  const res = await read(client.url(path), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  })
  if (!res.ok) throw new Error(await refusal(res))
}

/** Take one device's key away. The device's next request is refused. */
export function revokeDevice(id: string, read: typeof fetch = fetch): Promise<void> {
  return post("/v1/auth/devices/" + encodeURIComponent(id) + "/revoke", read)
}

/** Take this browser's own key away and clear its cookie. */
export function signOutThisBrowser(read: typeof fetch = fetch): Promise<void> {
  return post("/v1/auth/logout", read)
}
