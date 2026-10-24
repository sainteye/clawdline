import type { DeviceList } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core/refusal"

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
 *
 * Paths are same-origin, as the console's `client.url` makes them (it has no
 * base URL); importing that client here would keep node's test runner from
 * loading this file.
 */
export type SignedIn = { kind: "list"; list: DeviceList } | { kind: "device" } | { kind: "failed"; why: string }

/** The two grants this machine can give a directly paired device (`DevicePermission`). */
export type Permission = "read-only" | "read-and-operate"

/**
 * What a row's `caps` grant. Read alone is read-only; read with send is
 * read-and-operate. Anything else — no read, or a capability this console does
 * not know — is `unknown`, never shown as either grant.
 */
export function devicePermission(caps: readonly string[] | null | undefined): Permission | "unknown" {
  if (!Array.isArray(caps) || !caps.includes("read")) return "unknown"
  const rest = caps.filter((cap) => cap !== "read")
  if (rest.length === 0) return "read-only"
  if (rest.length === 1 && rest[0] === "send") return "read-and-operate"
  return "unknown"
}

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
    const res = await read("/v1/auth/devices", { cache: "no-store" })
    if (res.status === 403) return { kind: "device" }
    if (!res.ok) return { kind: "failed", why: await refusal(res) }
    return { kind: "list", list: (await res.json()) as DeviceList }
  } catch (error) {
    return { kind: "failed", why: error instanceof Error ? error.message : String(error) }
  }
}

async function post(path: string, read: typeof fetch, body: object = {}): Promise<void> {
  const res = await read(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (res.ok) return
  // A refused revoke, sign-out or grant keeps its code, so the block can say
  // which refusal it was (`failureSentence`) instead of the daemon's English.
  let answer: unknown = null
  try {
    answer = await res.json()
  } catch {
    // refusal-ok: a body that is not JSON is reported below as a transport failure with its status.
  }
  if (isRefusal(answer)) throw new RefusalError(res.status, answer, path)
  throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
}

/** Take one device's key away. The device's next request is refused. */
export function revokeDevice(id: string, read: typeof fetch = fetch): Promise<void> {
  return post("/v1/auth/devices/" + encodeURIComponent(id) + "/revoke", read)
}

/**
 * Change what one directly paired device may do. The daemon answers only
 * after the grant is saved; the caller reads the list again to show it.
 */
export function setDevicePermission(id: string, permission: Permission, read: typeof fetch = fetch): Promise<void> {
  return post("/v1/auth/devices/" + encodeURIComponent(id) + "/permission", read, { permission })
}

/**
 * What the list read again after an accepted grant says about it. A grant is
 * reported as saved only when the re-read row shows exactly what was asked
 * for; a missing row, another grant or an unreadable list is said as such.
 */
export type GrantOutcome =
  | { kind: "saved" }
  | { kind: "mismatch"; now: Permission | "unknown"; caps: string[] }
  | { kind: "unread"; why: string }

export function grantOutcome(next: SignedIn, id: string, asked: Permission): GrantOutcome {
  if (next.kind === "failed") return { kind: "unread", why: next.why }
  if (next.kind === "device") return { kind: "unread", why: "403" }
  const row = next.list.devices.find((device) => device.id === id)
  const caps = row?.caps ?? []
  const now = row ? devicePermission(caps) : "unknown"
  return now === asked ? { kind: "saved" } : { kind: "mismatch", now, caps }
}

/** Take this browser's own key away and clear its cookie. */
export function signOutThisBrowser(read: typeof fetch = fetch): Promise<void> {
  return post("/v1/auth/logout", read)
}
