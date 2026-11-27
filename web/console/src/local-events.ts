import type { StreamHandle, StreamHandlers, StreamTransport } from "@clawdline/core"

interface LocalEventConnection {
  handle: StreamHandle
  listeners: Set<StreamHandlers>
  connected: boolean
  sessions?: string
  tasks?: string
}

/** EventSource.CLOSED: the browser gave up on this connection and will not retry. */
const CLOSED = 2

export function closedForGood(cause: unknown): boolean {
  return (cause as { target?: { readyState?: unknown } } | null)?.target?.readyState === CLOSED
}

/** The local console fans out the daemon's existing event stream within this tab. */
export function shareLocalEvents(native: StreamTransport): StreamTransport {
  let connection: LocalEventConnection | undefined
  return {
    open(url, subscriber) {
      const handlers = { ...subscriber }
      if (!connection) {
        const listeners = new Set<StreamHandlers>()
        const current: LocalEventConnection = {
          listeners, connected: false,
          handle: native.open(url, {
            onOpen() {
              current.connected = true
              for (const listener of listeners) listener.onOpen?.()
            },
            onError(cause) {
              current.connected = false
              current.sessions = current.tasks = undefined
              // A refused answer (an HTTP error, a non-stream body) closes a
              // native EventSource for good; the next listener opens a new one.
              if (closedForGood(cause) && connection === current) connection = undefined
              for (const listener of listeners) listener.onError?.(cause)
            },
            onFrame(event, data) {
              if (event === "sessions") current.sessions = data
              if (event === "orchestrator") current.tasks = data
              for (const listener of listeners) listener.onFrame(event, data)
            },
          }),
        }
        connection = current
      }
      const current = connection
      current.listeners.add(handlers)
      if (current.connected) handlers.onOpen?.()
      if (current.sessions !== undefined) handlers.onFrame("sessions", current.sessions)
      if (current.tasks !== undefined) handlers.onFrame("orchestrator", current.tasks)
      return {
        close() {
          if (!current.listeners.delete(handlers) || current.listeners.size > 0) return
          if (connection === current) connection = undefined
          current.handle.close()
        },
      }
    },
  }
}
