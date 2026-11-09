import type { SessionDestination, SessionProjectionSource } from "./all-machine-sessions.js"

// Registered as console.started_session_retry_seconds and
// console.started_session_follow_seconds and console.started_session_reads.
const RETRY_MS = 5_000
const FOLLOW_MS = 60_000
const READ_LIMIT = 12

export type StartedSessionStopReason = "time_limit" | "read_limit"

/** Observe a confirmed launch; a missing event must not reuse a pre-launch list. */
export function observeStartedSession(source: SessionProjectionSource, sessionID: string,
  machines: () => readonly string[], accept: () => boolean,
  opened: (destination: SessionDestination) => void,
  onStopped?: (reason: StartedSessionStopReason) => void): () => void {
  const controller = new AbortController()
  const pending = new Map<string, { again: boolean; fresh: boolean }>()
  let settled = false
  let timer: ReturnType<typeof setTimeout> | undefined
  let deadline: ReturnType<typeof setTimeout> | undefined
  let reads = 0
  let unsubscribe = () => {}
  const stop = () => { controller.abort(); clearTimeout(timer); clearTimeout(deadline); unsubscribe() }
  const expire = (reason: StartedSessionStopReason) => {
    if (controller.signal.aborted || settled) return
    stop()
    onStopped?.(reason)
  }
  const findMachine = async (machineID: string, fresh = false) => {
    if (controller.signal.aborted || settled) return
    const held = pending.get(machineID)
    if (held) { held.again = true; held.fresh ||= fresh; return }
    if (reads >= READ_LIMIT) return
    const flight = { again: false, fresh: false }
    pending.set(machineID, flight)
    reads++
    try {
      const reading = await source.readMachine(machineID, controller.signal, fresh)
      if (controller.signal.aborted || settled) return
      const row = reading.kind === "ready" ? reading.rows?.find((item) => item.destination.machineID === machineID &&
        item.destination.sessionID === sessionID && item.freshness === "current") : undefined
      if (!row) return
      settled = true
      // Settlement always releases the launch even if navigation is no longer wanted.
      if (accept()) opened(row.destination)
      stop()
    } catch { /* Another machine can still contain the confirmed pane. */ }
    finally {
      pending.delete(machineID)
      if (reads >= READ_LIMIT && pending.size === 0) expire("read_limit")
      if (flight.again && !settled && !controller.signal.aborted && machines().includes(machineID)) {
        void findMachine(machineID, flight.fresh)
      }
    }
  }
  const find = (fresh = false) => { for (const machineID of machines()) void findMachine(machineID, fresh) }
  unsubscribe = source.subscribe((event) => {
    if (machines().includes(event.machineID) && (!event.sessionID || event.sessionID === sessionID)) void find()
  })
  const retry = () => {
    if (controller.signal.aborted || settled) return
    void find(true)
    timer = setTimeout(retry, RETRY_MS)
  }
  deadline = setTimeout(() => expire("time_limit"), FOLLOW_MS)
  timer = setTimeout(retry, RETRY_MS)
  void find(true)
  return stop
}
