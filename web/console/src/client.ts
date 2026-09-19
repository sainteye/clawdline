import { ClawdlineClient, nativeEventSourceTransport, type StreamTransport } from "@clawdline/core"

/**
 * One client for the whole console.
 *
 * It takes no base URL: the console is served by the daemon it talks to, and in
 * development Vite proxies /v1 to the same place. The code that runs in the
 * browser is therefore the same either way, and no build flag decides where the
 * data comes from.
 *
 * `fetch` is looked up when a request is made, not when this module loads. A
 * console built for Clawdline Cloud answers its own-origin `/v1/…` requests
 * from the relay (`cloud/install.ts`), and so do the panels that call `fetch`
 * themselves; one place decides that for both. Served by the daemon, this is
 * the browser's own `fetch`, as it always was.
 */
export const client = new ClawdlineClient({ fetch: (input, init) => globalThis.fetch(input, init) })

let fleetStream: StreamTransport | null = null

/**
 * The live connection the session list follows: the daemon's `/v1/events`, or
 * the relay's when the page reads a machine through Clawdline Cloud.
 */
export function fleetTransport(): StreamTransport | undefined {
  return fleetStream ?? nativeEventSourceTransport()
}

/** Follow `transport` instead of `/v1/events`. Set once, before the console is drawn. */
export function followFleetFrom(transport: StreamTransport): void {
  fleetStream = transport
}
