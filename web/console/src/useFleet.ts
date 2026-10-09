import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"
import { ClawdlineClient, FleetStore, RefusalError, TransportError, type FleetState } from "@clawdline/core"
import { fleetTransport } from "./client.js"
import {
  documentVisibility,
  initialPollState,
  Poller,
  type Backoff,
  type PollState,
  type ReadFailureKind,
  type ReadReason,
} from "./poll.js"

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
 * of announcing it (`session/transcript-trouble.ts`). `retry` asks now, and
 * `poke` asks now or right after the read that is out; the rules for when they
 * do not are `Poller`'s (`poll.ts`).
 *
 * Nothing is read while the page is hidden: the read that comes due waits, and
 * is made once when the page is seen again. A panel nobody can see has nobody
 * to show the answer to, and a phone in a pocket was reading every panel on
 * its old pace for as long as the tab stayed open.
 */
export function usePoll<T>(
  read: (why: ReadReason) => Promise<T>,
  intervalMs = 5000,
  options: { backoff?: Backoff } = {},
): PollState<T> & { retry: () => void; poke: () => void } {
  const [state, setState] = useState<PollState<T>>(initialPollState)
  const [poller, setPoller] = useState<Poller<T> | null>(null)
  // The interval is read once when the poller is made and handed over after,
  // so a page that changes pace keeps what it has read.
  const pace = useRef(intervalMs)
  pace.current = intervalMs

  const backoff = useRef(options.backoff)
  backoff.current = options.backoff

  useEffect(() => {
    const next = new Poller<T>({
      read,
      intervalMs: pace.current,
      classify: readFailureKind,
      onChange: setState,
      visibility: documentVisibility(),
      backoff: backoff.current,
    })
    setPoller(next)
    next.start()
    return () => next.stop()
    // read is expected to be stable; callers build it with useMemo.
  }, [read])

  useEffect(() => {
    poller?.setIntervalMs(intervalMs)
  }, [poller, intervalMs])

  const retry = useCallback(() => poller?.retry(), [poller])
  const poke = useCallback(() => poller?.poke(), [poller])
  return { ...state, retry, poke }
}

/** `TransportError` and `RefusalError` are the core's two answers; see `ReadFailureKind`. */
function readFailureKind(err: unknown): ReadFailureKind {
  if (err instanceof RefusalError) return "refused"
  if (err instanceof TransportError) return "unanswered"
  return "unknown"
}
