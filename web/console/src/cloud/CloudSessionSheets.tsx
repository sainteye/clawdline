import { useEffect } from "react"
import { Start, StartSheet } from "../session/Start.js"
import { Command, CommandSheet } from "../session/Command.js"
import { openNewWorkItem } from "../pages/work/new-item.js"
import { destinationFragment, type SessionProjectionSource } from "./all-machine-sessions.js"
import type { MachineToolbarAction } from "./FleetSessionList.js"

export interface PendingMachineAction {
  machineID: string
  action: MachineToolbarAction
  at: number
}

/** Mount the original start and voice command sheets for the Cloud Session page. */
export function CloudSessionSheets({ machineID, source, pending, onConsumed }: {
  machineID: string
  source: SessionProjectionSource
  pending: PendingMachineAction | null
  onConsumed: () => void
}) {
  useEffect(() => {
    let closed = false
    let active = 0
    let stopOpen: () => void = () => undefined
    const open = (sessionID: string) => {
      stopOpen()
      const mine = ++active
      let busy = false
      const find = async () => {
        if (closed || mine !== active || busy) return
        busy = true
        try {
          const reading = await source.readMachine(machineID, new AbortController().signal)
          if (closed || mine !== active) return
          const row = reading.rows?.find((item) => item.destination.machineID === machineID &&
            item.destination.sessionID === sessionID && item.freshness === "current")
          if (row) { location.hash = destinationFragment(row.destination); stopOpen() }
        } catch { /* the next status event can retry this exact destination */ }
        finally { busy = false }
      }
      stopOpen = source.subscribe((event) => {
        if (event.machineID === machineID && (!event.sessionID || event.sessionID === sessionID)) void find()
      })
      void find()
    }
    Start.host({ open, openId: () => null, refresh: () => undefined })
    Command.host({ open, refresh: () => undefined })
    return () => { closed = true; active++; stopOpen() }
  }, [machineID, source])

  useEffect(() => {
    if (!pending || pending.machineID !== machineID) return
    if (Date.now() - pending.at > 30_000) { onConsumed(); return }
    const frame = requestAnimationFrame(() => {
      onConsumed()
      switch (pending.action) {
        case "terminal": location.hash = "#page=sessions&mode=terminal"; break
        case "voice": Command.openAndListen(); break
        case "work": openNewWorkItem(); break
        case "start": Start.open(); break
        case "start_terminal": Start.openTerminal(); break
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [machineID, pending, onConsumed])

  return <><StartSheet /><CommandSheet /></>
}
