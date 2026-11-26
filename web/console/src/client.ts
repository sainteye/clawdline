import { ClawdlineClient, nativeEventSourceTransport, pollingTransport, type StreamHandle, type StreamTransport } from "@clawdline/core"
import { shareLocalEvents } from "./local-events.js"

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
const native = nativeEventSourceTransport(["sessions", "orchestrator", "screen"])
const localStream = native && shareLocalEvents(native)

/**
 * The live connection the session list follows: the daemon's `/v1/events`, or
 * the relay's when the page reads a machine through Clawdline Cloud.
 */
export function fleetTransport(): StreamTransport | undefined {
  return fleetStream ?? quietableTransport()
}

// A visible terminal has its own stream. Its tab polls the fleet while the
// terminal is open so there is capacity left for keystrokes and other reads.
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
const FLEET_STREAM_LOCK = "clawdline-local-fleet-stream"

function quietableTransport(): StreamTransport | undefined {
  const stream = localStream
  if (!stream) return undefined
  const reads = pollingTransport(async () => JSON.stringify(await client.sessions()), QUIET_READ_MS)
  return {
    open(url, handlers) {
      const locks = globalThis.navigator?.locks
      let inner: StreamHandle | undefined
      let using: StreamTransport | undefined
      let claim: AbortController | undefined
      let release: (() => void) | undefined
      const use = (transport: StreamTransport) => {
        if (using === transport) return
        inner?.close()
        using = transport
        inner = transport.open(url, handlers)
      }
      const change = () => {
        if (quiet > 0) {
          use(reads)
          claim?.abort()
          release?.()
          claim = undefined
          release = undefined
          return
        }
        if (!locks) { use(stream); return }
        if (claim) return
        const control = new AbortController()
        claim = control
        const hold = () => {
          if (control.signal.aborted) return
          return new Promise<void>(done => {
            release = done
            use(stream)
          })
        }
        // The first tab starts the live stream. Followers keep their own
        // authenticated reads and queue for the lease with no retry timer.
        let waiting = false
        void locks.request(FLEET_STREAM_LOCK, { ifAvailable: true }, lock => {
          if (control.signal.aborted) return
          if (lock) return hold()
          waiting = true
          use(reads)
        }).then(async () => {
          if (waiting && !control.signal.aborted) await locks.request(FLEET_STREAM_LOCK, { signal: control.signal }, hold)
        }).catch(() => {
          if (!control.signal.aborted) use(reads)
        })
      }
      quietChanged.add(change)
      change()
      return {
        close() {
          quietChanged.delete(change)
          inner?.close()
          claim?.abort()
          release?.()
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

/** Screen observations only exist on the machine's own event stream. */
export function watchLocalScreen(onFrame: (data: string) => void, onOpen?: () => void, onError?: () => void): StreamHandle | undefined {
  if (followsRelay()) return undefined
  return localStream?.open(client.url("/v1/events"), {
    onFrame: (event, data) => { if (event === "screen") onFrame(data) },
    onOpen, onError,
  })
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
