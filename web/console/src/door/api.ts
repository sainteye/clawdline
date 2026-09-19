import * as L from "../legacy/bridge.js"

/**
 * The door's three requests and the one read that decides whether it shows,
 * spelled as `net/live.js` spells them (`pair`, `confirmPair`, `password`,
 * `check`) and read through `net/fetch.js`'s `jsonFetch`: a refusal becomes an
 * Error carrying the server's `code` and its sentence, a server that is not
 * there becomes the catalog's `webOffline`.
 *
 * Not the shared client: these are the only calls a browser makes before it
 * is let in, their refusals are the Swift envelope (`{"error":{"code",
 * "message"}}`, auth.schema.json) rather than this daemon's flat one, and the
 * door reads `tries_left` from them, which nothing else does.
 */

export type DoorFailure = Error & { code?: string; triesLeft?: number }

export interface PairStarted {
  pairing_id: string
  expires: number
}

/** The two answers on the open health that the door needs (system.schema.json `Health`). */
export interface DoorHealth {
  authed?: boolean
  password?: boolean
}

async function jsonFetch<A>(path: string, init?: RequestInit): Promise<A> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    // "Failed to fetch" ends up in front of somebody as an explanation, so it
    // is turned into one here.
    const dead: DoorFailure = new Error(L.strings.webOffline)
    dead.code = "offline"
    throw dead
  }
  const body = await res.text()
  let data: unknown = null
  try {
    data = body ? JSON.parse(body) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; tries_left?: number } } | null)?.error ?? {
      code: "http_" + res.status,
      message: res.statusText || L.strings.webRequestFailed,
    }
    const e: DoorFailure = new Error(err.message || err.code)
    e.code = err.code
    if (typeof err.tries_left === "number") e.triesLeft = err.tries_left
    throw e
  }
  if (!data) throw new Error(L.strings.webNotJSON)
  return data as A
}

/** A POST of JSON, which is the shape every write on this server takes (`net/fetch.js` `post`). */
function post(body: unknown): RequestInit {
  return { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body ?? {}) }
}

export const doorApi = {
  pair: (name: string) => jsonFetch<PairStarted>("/v1/auth/pair", post({ name })),
  confirmPair: (id: string, code: string) =>
    jsonFetch<{ ok: boolean }>("/v1/auth/pair/confirm", post({ pairing_id: id, code })),
  password: (password: string, name: string) =>
    jsonFetch<{ ok: boolean }>("/v1/auth/password", post({ password, name })),
  /**
   * Never from the cache. This one answer decides whether the reader is shown
   * the door or the sessions, and a stale "yes you are signed in" is the wrong
   * answer to have kept.
   */
  health: () => jsonFetch<DoorHealth>("/v1/health", { cache: "no-store" }),
}
