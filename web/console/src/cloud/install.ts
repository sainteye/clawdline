import { followFleetFrom, followFleetWriteAccess } from "../client.js"
import { carryPictures } from "../legacy/images-bridge.js"
import type { StreamHandle, StreamHandlers, StreamTransport } from "@clawdline/core"
import type { RelayReader } from "./relay-reader.js"

/**
 * Point the console at a machine across the relay.
 *
 * Three things, and only these. Every `fetch` of this page's own `/v1/…` goes to
 * `reader` — `client` looks `fetch` up per request (`client.ts`), and the
 * panels that call `fetch` themselves reach the same place, so a route this
 * version does not carry is a typed `cloud_not_carried` refusal rather than
 * the static host's page coming back as a body that is not JSON. The session
 * list follows the relay instead of `/v1/events`. And a transcript's pictures
 * are read as bytes through the same seam (the Mac's `image` read) instead of
 * by an `<img>` pointed at a host that has none of them.
 *
 * Anything that is not this origin's `/v1/` — the catalog file, the pictures,
 * the API and relay themselves — goes to the network exactly as before.
 * Called once, before the console is drawn.
 */
let activeReader: RelayReader | null = null
let networkFetch: typeof globalThis.fetch | null = null
const fleetListeners = new Set<{ url: string; handlers: StreamHandlers; handle: StreamHandle }>()
const changingFleet: StreamTransport = {
  open(url, handlers) {
    const slot = { url, handlers, handle: activeReader!.stream().open(url, handlers) }
    fleetListeners.add(slot)
    return { close() { slot.handle.close(); fleetListeners.delete(slot) } }
  },
}

export function readThroughRelay(reader: RelayReader): void {
  activeReader = reader
  if (networkFetch) {
    for (const slot of fleetListeners) {
      slot.handle.close()
      slot.handle = reader.stream().open(slot.url, slot.handlers)
    }
    return
  }
  const network = globalThis.fetch.bind(globalThis)
  networkFetch = network
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const href = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    let url: URL
    try {
      url = new URL(href, location.href)
    } catch {
      return network(input, init)
    }
    if (url.origin === location.origin && url.pathname.startsWith("/v1/")) return activeReader!.fetch(input, init)
    return network(input, init)
  }
  followFleetFrom(changingFleet)
  followFleetWriteAccess(() => activeReader?.mayWrite() === true)
  carryPictures(async (artifact, session) => {
    const res = await activeReader!.fetch(
      "/v1/artifacts/images/" + encodeURIComponent(artifact.id) + "?session=" + encodeURIComponent(session),
    )
    if (!res.ok) {
      // The tile says the refusal in its own words, by its code
      // (`describeArtifactFailure`): a picture too large for one envelope says
      // its size rather than drawing a broken image.
      const body = (await res.json().catch(() => null)) as { error?: { code?: unknown } | string } | null
      const code = typeof body?.error === "string" ? body.error : body?.error?.code
      throw Object.assign(new Error("picture not carried"), { code: typeof code === "string" ? code : "read_failed" })
    }
    const url = URL.createObjectURL(await res.blob())
    return { url, release: () => URL.revokeObjectURL(url) }
  })
}
