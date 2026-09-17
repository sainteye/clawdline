import { useEffect, useMemo, useSyncExternalStore } from "react"
import { FleetStore, nativeEventSourceTransport, type FleetState } from "@clawdline/core"
import { client } from "../client.js"
import { BAR_HIDDEN_EVENT, BAR_SHOWN_EVENT, inShell } from "./shell.js"

/**
 * The fleet, read only while the bar is on screen.
 *
 * `useFleet` (the console's) opens the stream on mount and keeps it for the
 * life of the page, which is right for a window somebody is looking at. This
 * window is looked at for a few seconds at a time and stays loaded in between:
 * the shell orders it out rather than tearing it down, so that a summon is
 * instant. A stream left open across all of that is not free — the comment on
 * `fleetBridgeScript` in `shell/darwin/main.swift` states the cost plainly:
 * "Every stream a client opens makes the daemon take its own reading of the
 * machine — process table, tmux, screens — so a second connection from the
 * shell would double that work to learn what the page already knows."
 *
 * So the stream follows the window. `BAR_SHOWN_EVENT` starts it, which also
 * takes a fresh reading, and `BAR_HIDDEN_EVENT` stops it. Outside a shell there
 * is nothing to hide behind and it simply runs, which is what makes this page
 * developable in a browser.
 *
 * What is deliberately *not* done here is dropping the last snapshot on hide.
 * A summon opens on the list it had, a beat before the new one lands, rather
 * than on an empty card — the same reason `refreshProjectInfo` keeps its answer
 * between summons ("worth being a second stale to have the name there the
 * moment you look").
 */
export function useBarFleet(): FleetState {
  const store = useMemo(() => new FleetStore(client, nativeEventSourceTransport()), [])

  useEffect(() => {
    let running = false
    const start = () => {
      if (running) return
      running = true
      void store.start()
    }
    const stop = () => {
      if (!running) return
      running = false
      store.stop()
    }
    // A bar that is already on screen when this mounts — the first load, which
    // the shell does before the first summon — still needs its first reading,
    // so the shell's `shown` is what starts it and the browser starts at once.
    if (!inShell()) start()
    window.addEventListener(BAR_SHOWN_EVENT, start)
    window.addEventListener(BAR_HIDDEN_EVENT, stop)
    return () => {
      window.removeEventListener(BAR_SHOWN_EVENT, start)
      window.removeEventListener(BAR_HIDDEN_EVENT, stop)
      stop()
    }
  }, [store])

  return useSyncExternalStore(
    (fn) => store.subscribe(fn),
    () => store.get(),
  )
}
