/* Holds one screen's stored evidence: one load at a time, refreshed while the
 * window is live, cancelled when the route or scope changes or the page goes
 * away. A changed route key starts empty, so one scope's figures never flash
 * under another's, and a refused session clears everything at once. */
import { useCallback, useEffect, useReducer, useRef } from "react"
import { emptyStored, HEALTH_UI_LIMITS, loadStored, olderRead, planFor, runReads, storedReducer, type Refusal, type StoredState } from "./cloud"
import { routeKey, type HealthRoute } from "./route"

export function useStored(get: typeof fetch, apiOrigin: string, route: HealthRoute): { state: StoredState; refresh: () => void; loadOlder: () => void; loadOlderIncidents: () => void; refuse: (r: Refusal) => void } {
  const key = routeKey(route)
  const [state, dispatch] = useReducer(storedReducer, key, emptyStored)
  const inFlight = useRef<AbortController | null>(null)
  const retryAt = useRef(0)
  const routeRef = useRef(route)
  routeRef.current = route
  const stateRef = useRef(state)
  stateRef.current = state

  const run = useCallback(() => {
    if (inFlight.current || Date.now() < retryAt.current) return
    const ctl = new AbortController()
    inFlight.current = ctl
    dispatch({ type: "start" })
    void loadStored(get, apiOrigin, routeRef.current, Date.now(), ctl.signal).then((outcome) => {
      if (inFlight.current === ctl) inFlight.current = null
      if (!ctl.signal.aborted) {
        const delay = outcome.kind === "parts" ? Math.max(0, ...Object.values(outcome.parts).map(p => p && !p.ok && p.code === "rate_limited" ? p.retryAfterMs ?? 0 : 0)) : 0
        retryAt.current = Date.now() + delay
        dispatch({ type: "loaded", outcome, at: Date.now() })
      }
    })
  }, [get, apiOrigin])

  useEffect(() => {
    dispatch({ type: "reset", key })
    retryAt.current = 0
    run()
    return () => {
      inFlight.current?.abort()
      inFlight.current = null
    }
  }, [key, run])

  const live = route.scope.until === null
  useEffect(() => {
    if (!live) return
    const id = setInterval(run, HEALTH_UI_LIMITS.refreshMs)
    return () => clearInterval(id)
  }, [live, run, key])

  const loadOlder = useCallback(() => {
    const s = stateRef.current
    if (inFlight.current || s.olderBusy || s.olderCursor === null || s.window === null) return
    const first = planFor(routeRef.current, Date.now()).reads.find((r) => r.part === "trace" || r.part === "events")
    if (!first) return
    const ctl = new AbortController()
    inFlight.current = ctl
    dispatch({ type: "older_start" })
    void runReads(get, apiOrigin, [olderRead(first, s.window, s.olderCursor)], ctl.signal).then((r) => {
      if (inFlight.current === ctl) inFlight.current = null
      if (ctl.signal.aborted || r.kind === "aborted") return
      if (r.kind === "refused") dispatch({ type: "older_refused", refusal: r.refusal })
      else dispatch({ type: "older_loaded", result: r.parts[first.part] ?? { ok: false, code: "network" }, at: Date.now() })
    })
  }, [get, apiOrigin])

  // The next incident page: the same read, its own window and filter, and the Cloud's cursor.
  const loadOlderIncidents = useCallback(() => {
    const s = stateRef.current
    if (inFlight.current || s.incBusy || s.incCursor === null) return
    const first = planFor(routeRef.current, Date.now()).reads.find((r) => r.part === "incidents")
    if (!first) return
    const ctl = new AbortController()
    inFlight.current = ctl
    dispatch({ type: "inc_older_start" })
    void runReads(get, apiOrigin, [olderRead(first, { from: first.params.from, to: first.params.to }, s.incCursor)], ctl.signal).then((r) => {
      if (inFlight.current === ctl) inFlight.current = null
      if (ctl.signal.aborted || r.kind === "aborted") return
      if (r.kind === "refused") dispatch({ type: "older_refused", refusal: r.refusal })
      else dispatch({ type: "inc_older_loaded", result: r.parts.incidents ?? { ok: false, code: "network" }, at: Date.now() })
    })
  }, [get, apiOrigin])

  // A refusal met outside a load (an export or an acknowledgement) clears everything just the same.
  const refuse = useCallback((r: Refusal) => {
    inFlight.current?.abort()
    inFlight.current = null
    retryAt.current = 0
    dispatch({ type: "older_refused", refusal: r })
  }, [])

  return { state, refresh: run, loadOlder, loadOlderIncidents, refuse }
}
