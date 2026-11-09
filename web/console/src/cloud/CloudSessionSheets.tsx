import { useEffect, useRef } from "react"
import { Start } from "../session/Start.js"
import { Command } from "../session/Command.js"
import { openNewWorkItem } from "../pages/work/new-item.js"
import type { SessionDestination, SessionProjectionSource } from "./all-machine-sessions.js"
import { observeStartedSession } from "./started-session.js"
import { toolPlan } from "./machine-tool.js"
import type { StartAnswer, StartPlaceRow } from "../legacy/start-bridge.js"
import type { MachineToolbarAction } from "./FleetSessionList.js"

export interface PendingMachineAction {
  machineID: string
  /** The machine's own name, for a sheet that has to say where it will start. */
  machineName?: string
  action: MachineToolbarAction
  at: number
}

/** Connect Cloud actions to the sheets already mounted by App's SessionsPage. */
export function CloudSessionSheets({ machineID, machines, source, pending, onConsumed, onOpened, onStartConfirmed }: {
  machineID: string
  /**
   * Every machine the fleet is showing. A Session started from the fleet is
   * started on the machine the person picked, which is not always the one this
   * console reads, so the row that arrives is looked for on all of them.
   */
  machines?: readonly string[]
  source: SessionProjectionSource
  pending: PendingMachineAction | null
  onConsumed: () => void
  /**
   * The Session a start or a voice command produced, once its row has been
   * read and its execution named. Where it opens is the gate's to decide: the
   * fleet opens it in place, and a one-machine console opens it at the
   * original address.
   */
  onOpened: (destination: SessionDestination) => void
  onStartConfirmed?: (answer: StartAnswer, place: StartPlaceRow | null) => void
}) {
  // The machines to look through, kept in a ref so that the lookup installed
  // below survives a list that changes while a start is in flight.
  const looking = useRef<readonly string[]>([machineID])
  looking.current = machines?.length ? [machineID, ...machines.filter((id) => id !== machineID)] : [machineID]
  // The lookup below is installed once and outlives this render.
  const opened = useRef(onOpened)
  opened.current = onOpened
  const confirmed = useRef(onStartConfirmed)
  confirmed.current = onStartConfirmed

  useEffect(() => {
    let closed = false
    let active = 0
    let starting = false
    let stopOpen: () => void = () => undefined
    const open = (sessionID: string, accept: () => boolean = () => true, machine?: string, stopped?: () => void) => {
      starting = false
      stopOpen()
      const mine = ++active
      stopOpen = observeStartedSession(source, sessionID, () => machine ? [machine] : looking.current,
        () => !closed && mine === active && accept(),
        (destination) => Start.afterProgressHidden(() => opened.current(destination)), stopped)
    }
    const stopWait = Start.subscribe(() => {
      if (starting && !Start.progress()) stopOpen()
    })
    Start.host({ open, openId: () => null, refresh: () => undefined,
      openStarted: (answer, place, finish) => {
        if (!answer.id) return false
        confirmed.current?.(answer, place)
        const from = location.hash
        open(answer.id, () => {
          const navigate = finish()
          return location.hash === from && navigate
        }, place?.machine || machineID, () => Start.dismiss())
        starting = true
        return true
      },
    })
    Command.host({ open, refresh: () => undefined })
    return () => {
      closed = true; active++; stopOpen(); stopWait()
      if (starting) Start.dismiss()
    }
  }, [machineID, source])

  useEffect(() => {
    if (!pending) return
    // What the press means now that a machine has been named (`machine-tool.ts`).
    const plan = toolPlan({ pressed: pending, reading: machineID, now: Date.now() })
    if (plan.do === "wait") return
    if (plan.do === "forget") { onConsumed(); return }
    const frame = requestAnimationFrame(() => {
      onConsumed()
      switch (plan.action) {
        case "terminal": location.hash = "#page=sessions&mode=terminal"; break
        case "voice": Command.openAndListen(); break
        case "work": openNewWorkItem(); break
        case "start":
          if (plan.do === "elsewhere") Start.openOn({ id: plan.machine, name: pending.machineName || plan.machine })
          else Start.open()
          break
        case "start_terminal": Start.openTerminal(); break
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [machineID, pending, onConsumed])

  return null
}
