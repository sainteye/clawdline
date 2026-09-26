import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"
import { ClawdlineClient, FleetStore, RefusalError, TransportError, type FleetState } from "@clawdline/core"
import { fleetTransport } from "./client.js"
import { initialPollState, Poller, type PollState, type ReadFailureKind } from "./poll.js"

/**
 * Binds the framework-free store to React.
 *
 * The rules about which snapshot wins live in the store, not here. This file is
 * the only part of the fleet list a React Native app would have to write again,
 * and it is nine lines.
 */
export function useFleet(client: ClawdlineClient): FleetState & { refresh: () => void } {
  const store = useMemo(() => new FleetStore(client, fleetTransport()), [client])
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
 *
 * A failed read keeps what it threw and says which kind it was, and how long
 * the reads have been failing, so a panel can wait out one missed poll instead
 * of announcing it (`session/transcript-trouble.ts`). `retry` asks now; the
 * rules for when it does not are `Poller`'s (`poll.ts`).
 */
export function usePoll<T>(read: () => Promise<T>, intervalMs = 5000): PollState<T> & { retry: () => void } {
  const [state, setState] = useState<PollState<T>>(initialPollState)
  const [poller, setPoller] = useState<Poller<T> | null>(null)
  // The interval is read once when the poller is made and handed over after,
  // so a page that changes pace keeps what it has read.
  const pace = useRef(intervalMs)
  pace.current = intervalMs

  useEffect(() => {
    const next = new Poller<T>({ read, intervalMs: pace.current, classify: readFailureKind, onChange: setState })
    setPoller(next)
    next.start()
    return () => next.stop()
    // read is expected to be stable; callers build it with useMemo.
  }, [read])

  useEffect(() => {
    poller?.setIntervalMs(intervalMs)
  }, [poller, intervalMs])

  const retry = useCallback(() => poller?.retry(), [poller])
  return { ...state, retry }
}

/** `TransportError` and `RefusalError` are the core's two answers; see `ReadFailureKind`. */
function readFailureKind(err: unknown): ReadFailureKind {
  if (err instanceof RefusalError) return "refused"
  if (err instanceof TransportError) return "unanswered"
  return "unknown"
}
