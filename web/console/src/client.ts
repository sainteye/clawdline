import { ClawdlineClient, nativeEventSourceTransport, pollingTransport, type StreamTransport } from "@clawdline/core"

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
let fleetMayWrite: (() => boolean) | null = null

/**
 * The live connection the session list follows: the daemon's `/v1/events`, or
 * the relay's when the page reads a machine through Clawdline Cloud.
 */
export function fleetTransport(): StreamTransport | undefined {
  return fleetStream ?? quietableTransport()
}

// A browser opens about six connections to one host at a time, and a stream
// holds one for as long as it is open. A terminal on screen holds its own
// stream (pages/terminal), so while one is shown this tab reads the session
// list every few seconds instead of holding `/v1/events` open as well: three
// terminal tabs and two console tabs are then five held connections, not
// eight, and there is still one left for the keystrokes.
let quiet = 0
const quietChanged = new Set<() => void>()

/** Read the session list instead of following its stream until the returned function is called. */
export function quietFleetStream(): () => void {
  quiet += 1
  for (const fn of quietChanged) fn()
  let done = false
  return () => {
    if (done) return
    done = true
    quiet -= 1
    for (const fn of quietChanged) fn()
  }
}

/** Every few seconds while quiet, the stream otherwise; the switch reopens with the same handlers. */
const QUIET_READ_MS = 5_000

function quietableTransport(): StreamTransport | undefined {
  const stream = nativeEventSourceTransport(["sessions", "orchestrator"])
  if (!stream) return undefined
  const reads = pollingTransport(async () => JSON.stringify(await client.sessions()), QUIET_READ_MS)
  return {
    open(url, handlers) {
      let reading = quiet > 0
      let inner = (reading ? reads : stream).open(url, handlers)
      const change = () => {
        if (reading === quiet > 0) return
        reading = quiet > 0
        inner.close()
        inner = (reading ? reads : stream).open(url, handlers)
      }
      quietChanged.add(change)
      return {
        close() {
          quietChanged.delete(change)
          inner.close()
        },
      }
    },
  }
}

/**
 * Whether the page reads its machine through Clawdline Cloud. There, this
 * origin's `/v1/events` is the static host's and not the machine's, so a panel
 * that would open its own EventSource asks the machine instead.
 */
export function followsRelay(): boolean {
  return fleetStream !== null
}

/** A presentation hint; the daemon still checks every write. */
export function mayWriteThroughCurrentTransport(): boolean {
  return fleetMayWrite?.() ?? true
}

/** Follow `transport` instead of `/v1/events`. Set once, before the console is drawn. */
export function followFleetFrom(transport: StreamTransport): void {
  fleetStream = transport
}

export function followFleetWriteAccess(read: () => boolean): void {
  fleetMayWrite = read
}
