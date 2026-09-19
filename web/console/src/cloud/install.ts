import { followFleetFrom } from "../client.js"
import type { RelayReader } from "./relay-reader.js"

/**
 * Point the console at a machine across the relay.
 *
 * Two things, and only these. Every `fetch` of this page's own `/v1/…` goes to
 * `reader` — `client` looks `fetch` up per request (`client.ts`), and the
 * panels that call `fetch` themselves reach the same place, so a route this
 * version does not carry is a typed `cloud_not_carried` refusal rather than
 * the static host's page coming back as a body that is not JSON. And the
 * session list follows the relay instead of `/v1/events`.
 *
 * Anything that is not this origin's `/v1/` — the catalog file, the pictures,
 * the API and relay themselves — goes to the network exactly as before.
 * Called once, before the console is drawn.
 */
export function readThroughRelay(reader: RelayReader): void {
  const network = globalThis.fetch.bind(globalThis)
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const href = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    let url: URL
    try {
      url = new URL(href, location.href)
    } catch {
      return network(input, init)
    }
    if (url.origin === location.origin && url.pathname.startsWith("/v1/")) return reader.fetch(input, init)
    return network(input, init)
  }
  followFleetFrom(reader.stream())
}
