import type { SessionDestination, SessionProjectionSource } from "./all-machine-sessions.js"

/** Observe a confirmed launch; a missing event must not reuse a pre-launch list. */
export function observeStartedSession(source: SessionProjectionSource, sessionID: string,
  machines: () => readonly string[], accept: () => boolean,
  opened: (destination: SessionDestination) => void): () => void {
  const controller = new AbortController()
  let busy = false
  let again = false
  let settled = false
  let unsubscribe = () => {}
  const stop = () => { controller.abort(); unsubscribe() }
  const find = async (fresh = false) => {
    if (controller.signal.aborted || settled) return
    if (busy) { again = true; return }
    busy = true
    try {
      for (const machineID of machines()) {
        try {
          const reading = await source.readMachine(machineID, controller.signal, fresh)
          if (controller.signal.aborted) return
          const row = reading.kind === "ready" ? reading.rows?.find((item) => item.destination.machineID === machineID &&
            item.destination.sessionID === sessionID && item.freshness === "current") : undefined
          if (!row) continue
          settled = true
          // Settlement always releases the launch even if navigation is no longer wanted.
          if (accept()) opened(row.destination)
          stop()
          return
        } catch { /* Another machine can still contain the confirmed pane. */ }
      }
    } finally {
      busy = false
      if (again && !settled) { again = false; void find() }
    }
  }
  unsubscribe = source.subscribe((event) => {
    if (machines().includes(event.machineID) && (!event.sessionID || event.sessionID === sessionID)) void find()
  })
  void find(true)
  return stop
}
