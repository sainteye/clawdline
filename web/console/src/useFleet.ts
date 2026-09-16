import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react"
import {
  ClawdlineClient,
  FleetStore,
  nativeEventSourceTransport,
  type FleetState,
} from "@clawdline/core"

/**
 * Binds the framework-free store to React.
 *
 * The rules about which snapshot wins live in the store, not here. This file is
 * the only part of the fleet list a React Native app would have to write again,
 * and it is nine lines.
 */
export function useFleet(client: ClawdlineClient): FleetState & { refresh: () => void } {
  const store = useMemo(() => new FleetStore(client, nativeEventSourceTransport()), [client])
  useEffect(() => {
    void store.start()
    return () => store.stop()
  }, [store])
  const state = useSyncExternalStore(
    (fn) => store.subscribe(fn),
    () => store.get(),
  )
  // An action changes the machine, and the next stream frame may be a second
  // away. Asking once, immediately, is what makes a button feel like it did
  // something — and it goes through the same accept() rule, so a stale reading
  // racing a fresh one cannot win.
  const refresh = useCallback(() => {
    void store.refresh()
  }, [store])
  return { ...state, refresh }
}

/**
 * Reads one route on an interval.
 *
 * `pending` starts true and only ever goes false, so a panel can tell "not
 * asked yet" from "asked and the answer was empty" — the same distinction the
 * scan payload makes, applied to the panels that have no scan of their own.
 */
export function usePoll<T>(read: () => Promise<T>, intervalMs = 5000): {
  data: T | null
  error: string | null
  pending: boolean
} {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(true)

  useEffect(() => {
    let alive = true
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      try {
        const next = await read()
        if (!alive) return
        setData(next)
        setError(null)
      } catch (err) {
        if (!alive) return
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (alive) {
          setPending(false)
          timer = setTimeout(tick, intervalMs)
        }
      }
    }
    void tick()
    return () => {
      alive = false
      clearTimeout(timer)
    }
    // read is expected to be stable; callers build it with useMemo.
  }, [read, intervalMs])

  return { data, error, pending }
}
