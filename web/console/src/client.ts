import { ClawdlineClient, RefusalError, nativeEventSourceTransport, pollingTransport, type StreamHandle, type StreamHandlers, type StreamTransport } from "@clawdline/core"
import { closedForGood, shareLocalEvents } from "./local-events.js"

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
const fleetRetryRequested = new Set<() => void>()

/** Reconnect a local fleet stream whose retry is pending or was refused access. */
export function retryFleetStream(): void {
  if (fleetStream) return
  for (const retry of fleetRetryRequested) retry()
}

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

// An HTTP error closes an EventSource for good. The holder lets the lock go,
// waits, reads once to check access, and opens again. The wait doubles up to
// the ceiling and stays there until the stream opens or the person retries.
export const FLEET_RECONNECT_FIRST_MS = 1_000
export const FLEET_RECONNECT_CEILING_MS = 30_000
/** The daemon refused this page's access: retrying will not help until the person signs in again. */
const accessRefused = (cause: unknown) => cause instanceof RefusalError && (cause.status === 401 || cause.status === 403)

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

interface ReconnectLadder {
  /** The stream was refused; the lock is already let go. */
  refused(): void
  /** The stream opened: the next failure waits the first interval. */
  opened(): void
  pending(): boolean
  accessStopped(): boolean
  stopForAccess(cause: unknown): boolean
  reset(): boolean
  /** Whether a retry is pending or the page stopped and stays hidden. */
  holds(hidden: boolean): boolean
  close(): void
}

function reconnectLadder(
  read: () => ReturnType<typeof client.sessions>,
  reopen: () => void,
  pause: () => void,
): ReconnectLadder {
  let wait = FLEET_RECONNECT_FIRST_MS
  let retry: { timer: ReturnType<typeof setTimeout> } | undefined
  let stopped = false
  const close = () => {
    clearTimeout(retry?.timer)
    retry = undefined
  }
  const reset = () => {
    const recovering = retry !== undefined || stopped
    close()
    stopped = false
    wait = FLEET_RECONNECT_FIRST_MS
    return recovering
  }
  const stopForAccess = (cause: unknown) => {
    if (!retry || !accessRefused(cause)) return false
    reset()
    stopped = true
    pause()
    return true
  }
  const refused = () => {
    const pending = { timer: setTimeout(() => void probe(pending), wait) }
    retry = pending
    wait = Math.min(wait * 2, FLEET_RECONNECT_CEILING_MS)
  }
  const probe = async (pending: NonNullable<typeof retry>) => {
    try {
      await read()
    } catch (cause) {
      if (retry === pending && !stopForAccess(cause)) refused()
      return
    }
    if (retry !== pending) return
    retry = undefined
    reopen()
  }
  return {
    refused,
    opened() { wait = FLEET_RECONNECT_FIRST_MS },
    pending: () => retry !== undefined,
    accessStopped: () => stopped,
    stopForAccess,
    reset,
    holds(hidden) {
      if (retry) return true
      // A page shown again, or a terminal closed, is a person's new try.
      if (stopped && hidden) return true
      stopped = false
      return false
    },
    close,
  }
}

function fleetRecovery(handlers: StreamHandlers, polling: () => boolean, reopen: () => void, pause: () => void) {
  // The fallback poll and the access probe can come due together.
  let reading: ReturnType<typeof client.sessions> | undefined
  const read = () => {
    if (reading) return reading
    const pending = client.sessions().finally(() => {
      if (reading === pending) reading = undefined
    })
    reading = pending
    return pending
  }
  const reads = pollingTransport(async () => JSON.stringify(await read()), QUIET_READ_MS)
  const ladder = reconnectLadder(read, reopen, pause)
  const polled: StreamHandlers = {
    ...handlers,
    onOpen() {
      if (polling() && !ladder.pending()) handlers.onOpen?.()
    },
    onError(cause) {
      handlers.onError?.(cause)
      ladder.stopForAccess(cause)
    },
  }
  const retryNow = () => {
    if (!ladder.reset()) return
    // A probe started before a manual retry cannot decide its outcome.
    reading = undefined
    reopen()
  }
  return { reads, polled, ladder, retryNow }
}

function quietableTransport(): StreamTransport | undefined {
  const stream = localStream
  if (!stream) return undefined
  return {
    open(url, handlers) {
      const locks = globalThis.navigator?.locks
      let inner: StreamHandle | undefined
      let using: StreamTransport | undefined
      let lease: FleetLease | undefined
      let closed = false
      // Reopen by visibility: a shown page steals the stream back from
      // whichever tab took it meanwhile, a hidden one queues behind it.
      const { reads, polled, ladder, retryNow } = fleetRecovery(
        handlers, () => !closed && using === reads, () => change(), () => use(paused),
      )
      const watched: StreamHandlers = {
        ...handlers,
        onOpen() {
          ladder.opened()
          handlers.onOpen?.()
        },
        onError(cause) {
          handlers.onError?.(cause)
          if (closed || using !== stream || !closedForGood(cause)) return
          // Let another tab take the stream at once; this one tries again later.
          drop()
          ladder.refused()
          waiting()
        },
      }
      const use = (transport: StreamTransport) => {
        if (using === transport) return
        inner?.close()
        using = transport
        inner = transport.open(url, transport === stream ? watched : polled)
      }
      const waiting = () => use(hidden() || ladder.accessStopped() ? paused : reads)
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
        if (ladder.holds(hidden())) {
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
      fleetRetryRequested.add(retryNow)
      page?.addEventListener("visibilitychange", change)
      change()
      return {
        close() {
          closed = true
          yielding = undefined
          ladder.close()
          quietChanged.delete(change)
          fleetRetryRequested.delete(retryNow)
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
