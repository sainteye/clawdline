/**
 * The live connection, without naming who provides it.
 *
 * This is the one place the two front ends genuinely differ. A browser has
 * EventSource; React Native does not, and its usual replacements (a fetch
 * stream, a websocket, a polling loop) have different shapes. So the shape
 * this package depends on is declared here, structurally, and each host
 * supplies it. Nothing below imports a DOM type.
 */
export interface StreamHandle {
  close(): void
}

export interface StreamHandlers {
  /** One decoded frame. `event` is the SSE event name, "message" when unnamed. */
  onFrame(event: string, data: string): void
  /** The connection came up. Called again on every successful reconnect. */
  onOpen?(): void
  /**
   * The connection dropped. This is not a statement about the daemon being
   * gone: a reader that treated a dropped stream as "everything ended" would
   * make exactly the mistake the scan payload exists to prevent.
   */
  onError?(cause: unknown): void
}

export interface StreamTransport {
  open(url: string, handlers: StreamHandlers): StreamHandle
}

/** The minimum of EventSource this package uses, declared without the DOM. */
interface EventSourceLike {
  addEventListener(type: string, listener: (ev: { data?: string }) => void): void
  close(): void
  onerror: ((ev: unknown) => void) | null
  onopen: ((ev: unknown) => void) | null
}

type EventSourceCtor = new (url: string) => EventSourceLike

/**
 * The transport a browser already has.
 *
 * Returns undefined rather than throwing where EventSource is absent, so a
 * host can ask for it and fall back without a try/catch around startup.
 */
export function nativeEventSourceTransport(
  eventNames: readonly string[] = ["sessions"],
): StreamTransport | undefined {
  const ctor = (globalThis as { EventSource?: EventSourceCtor }).EventSource
  if (!ctor) return undefined
  return {
    open(url, handlers) {
      const es = new ctor(url)
      es.onopen = () => handlers.onOpen?.()
      es.onerror = (cause) => handlers.onError?.(cause)
      es.addEventListener("message", (ev) => handlers.onFrame("message", ev.data ?? ""))
      for (const name of eventNames) {
        es.addEventListener(name, (ev) => handlers.onFrame(name, ev.data ?? ""))
      }
      return { close: () => es.close() }
    },
  }
}

/**
 * A transport built from repeated reads, for a host with no stream of its own.
 *
 * It is deliberately not a silent fallback. A screen fed by polling is a
 * different thing from a screen fed by a stream — it is later, and it can miss
 * a state that opened and closed between two reads — so a host that ends up
 * here should be able to say so.
 */
export function pollingTransport(
  read: () => Promise<string>,
  intervalMs = 2000,
  eventName = "sessions",
): StreamTransport {
  return {
    open(_url, handlers) {
      let stopped = false
      let timer: ReturnType<typeof setTimeout> | undefined
      const tick = async () => {
        if (stopped) return
        try {
          const body = await read()
          if (!stopped) handlers.onFrame(eventName, body)
        } catch (cause) {
          if (!stopped) handlers.onError?.(cause)
        }
        if (!stopped) timer = setTimeout(tick, intervalMs)
      }
      void tick().then(() => handlers.onOpen?.())
      return {
        close() {
          stopped = true
          if (timer) clearTimeout(timer)
        },
      }
    },
  }
}
