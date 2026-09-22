import * as L from "../legacy/bridge.js"
import { makeJSONFetch } from "@clawdline/core/refusal"

/**
 * The door's three requests and the one read that decides whether it shows,
 * spelled as `net/live.js` spells them (`pair`, `confirmPair`, `password`,
 * `check`) and read through `net/fetch.js`'s `jsonFetch`: a refusal becomes an
 * Error carrying the server's `code` and its sentence, a server that is not
 * there becomes the catalog's `webOffline`.
 *
 * These are the only calls a browser makes before it is let in. They use the
 * shared JSON transport with the door's catalog and keep the one extra field
 * this page reads: `tries_left`.
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

const jsonFetch = makeJSONFetch({
  words: {
    offline: L.strings.webOffline,
    requestFailed: L.strings.webRequestFailed,
    notJSON: L.strings.webNotJSON,
  },
  refusalFields: [{ source: "tries_left", target: "triesLeft", type: "number" }],
})

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
