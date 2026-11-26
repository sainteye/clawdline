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

// Read once per tab, as `native` reads EventSource: a browser tab keeps its document.
const page = (globalThis as { document?: EventTarget & { visibilityState?: string } }).document
const hidden = () => page?.visibilityState === "hidden"
/** A hidden tab that waits for the lease reads nothing; showing it reads or streams at once. */
const paused: StreamTransport = { open: () => ({ close() {} }) }

interface FleetLease {
  control: AbortController
  steal: boolean
  held: boolean
  release?: () => void
  settled?: Promise<unknown>
}

interface FleetLeaseEvents {
  /** The lock is held: stream. */
  held(): void
  /** Another tab holds or waits for it; this request is queued. */
  queued(): void
  /** Another visible tab stole it after it was held. */
  stolen(): void
  /** The browser refused the request: stream without the lock. */
  refused(): void
}

// A visible tab steals the lease so it streams at once. A hidden one takes it
// only when nobody holds or waits, and otherwise queues.
function requestFleetLease(locks: LockManager, steal: boolean, on: FleetLeaseEvents): FleetLease {
  const control = new AbortController()
  const lease: FleetLease = { control, steal, held: false }
  const hold = () => {
    if (control.signal.aborted) return
    return new Promise<void>(done => {
      lease.held = true
      lease.release = done
      on.held()
    })
  }
  const failed = (cause: unknown) => {
    if (control.signal.aborted) return
    if (lease.held && (cause as { name?: string } | null)?.name === "AbortError") on.stolen()
    else on.refused()
  }
  if (steal) {
    lease.settled = locks.request(FLEET_STREAM_LOCK, { steal: true }, hold).catch(failed)
    return lease
  }
  let queued = false
  lease.settled = locks.request(FLEET_STREAM_LOCK, { ifAvailable: true }, lock => {
    if (control.signal.aborted) return
    if (lock) return hold()
    queued = true
    on.queued()
  }).then(async () => {
    if (queued && !control.signal.aborted) await locks.request(FLEET_STREAM_LOCK, { signal: control.signal }, hold)
  }).catch(failed)
  return lease
}

function quietableTransport(): StreamTransport | undefined {
  const stream = localStream
  if (!stream) return undefined
  const reads = pollingTransport(async () => JSON.stringify(await client.sessions()), QUIET_READ_MS)
  return {
    open(url, handlers) {
      const locks = globalThis.navigator?.locks
      let inner: StreamHandle | undefined
      let using: StreamTransport | undefined
      let lease: FleetLease | undefined
      let closed = false
      const use = (transport: StreamTransport) => {
        if (using === transport) return
        inner?.close()
        using = transport
        inner = transport.open(url, handlers)
      }
      const waiting = () => use(hidden() ? paused : reads)
      const drop = () => {
        const previous = lease
        lease = undefined
        previous?.control.abort()
        previous?.release?.()
        return previous
      }
      const claim = (steal: boolean) => {
        if (!locks) return
        drop()
        const current = requestFleetLease(locks, steal, {
          held() {
            use(stream)
            // Hidden while the steal was granted: offer it to a visible tab.
            if (steal && hidden()) queueMicrotask(change)
          },
          queued: waiting,
          // Queue behind the tab that took it, without stealing it back.
          stolen() { if (lease === current) claim(false) },
          refused() { if (lease === current) use(stream) },
        })
        lease = current
      }
      // A hidden holder lets the lock go and asks again behind any waiter. The
      // request settles after the release, so a lone tab gets it straight back
      // and keeps the stream it has.
      let yielding: object | undefined
      const yieldLease = () => {
        const settled = drop()?.settled
        const turn = {}
        yielding = turn
        void Promise.resolve(settled).then(() => {
          if (yielding !== turn) return
          yielding = undefined
          change()
        })
      }
      const change = () => {
        if (closed) return
        if (quiet > 0) {
          yielding = undefined
          drop()
          waiting()
          return
        }
        if (!locks) { use(stream); return }
        if (yielding) return
        const current = lease
        if (hidden()) {
          if (current?.held) yieldLease()
          else if (!current) claim(false)
          else if (using === reads) waiting()
          return
        }
        if (current?.held || current?.steal) return
        claim(true)
      }
      quietChanged.add(change)
      page?.addEventListener("visibilitychange", change)
      change()
      return {
        close() {
          closed = true
          yielding = undefined
          quietChanged.delete(change)
          page?.removeEventListener("visibilitychange", change)
          inner?.close()
          drop()
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
