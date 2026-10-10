import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { ProjectedRow } from "../session/List.js"
import { Swipes } from "../session/swipe.js"
import { useSwipeToEnd } from "../session/use-swipe.js"
import type { OrderHold } from "../session/order.js"
import { SessionToolbar } from "../session/SessionToolbar.js"
import { ScheduleSection } from "../pages/schedules.js"
import { destinationKey, displayedPresentationStatus, presentationPass, projectionRefreshAt, settleProjection,
  type MachineSessionProjection, type ProjectionProblem, type SessionDestination,
  type SessionListPresentation, type SessionProjectionSource, type FleetMachine } from "./all-machine-sessions.js"
import { STATUS_FRESH_MS } from "./status-projection.js"
import { arrangeFleetRows, fleetWaitingKey } from "./fleet-order.js"
import { matchesFleetFilter, type FleetFilter } from "./fleet-filters.js"

export type MachineToolbarAction = "terminal" | "voice" | "work" | "start" | "start_terminal"
type Reading = { phase: "loading" } | { phase: "settled"; value: MachineSessionProjection }

/** Only the fleet list is new; the conversation and composer belong to SessionsPage. */
export function FleetSessionList({ machines, source, target, filter, onFilter, onOpen,
  onMachineAction, onCloseRequest }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  target: SessionDestination | null
  filter: string
  onFilter: (value: string) => void
  onOpen: (target: SessionDestination) => void
  onMachineAction?: (machineID: string, action: MachineToolbarAction) => void
  onCloseRequest: (target: SessionDestination, title: string) => void
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const presentations = useRef(new Map<string, { machineID: string; sessionID: string; pass: string } & SessionListPresentation>())
  const presentationPasses = useRef(new Map<string, string>())
  const presentationFailures = useRef(new Map<string, string>())
  const [presentationRevision, refreshPresentation] = useState(0)
  const [, redraw] = useState(0)
  const [machineAction, setMachineAction] = useState<MachineToolbarAction | null>(null)
  const [statusFilter, setStatusFilter] = useState<FleetFilter>("all")
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const machineDialog = useRef<HTMLDialogElement | null>(null)
  const searchField = useRef<HTMLInputElement | null>(null)
  const listScroll = useRef<HTMLDivElement | null>(null)
  const swipe = useRef(new Swipes()).current
  const swiped = useSwipeToEnd(listScroll, swipe)
  const orderHolds = useRef(new Map<string, OrderHold>())
  const [, redrawOrder] = useState(0)

  // The original Session list hands all its canvases to one page clock after
  // every render. Fleet rows use the same clock, including after status updates.
  useEffect(() => {
    L.registerSpinners([...(listScroll.current?.querySelectorAll<HTMLCanvasElement>("canvas.spin") ?? [])])
  })

  useEffect(() => {
    if (machineAction) machineDialog.current?.showModal()
    else machineDialog.current?.close()
  }, [machineAction])

  useEffect(() => {
    if (!source) {
      setReadings(Object.fromEntries(machines.map((machine) => [machine.id, {
        phase: "settled", value: { kind: "unavailable", reason: "old_version" },
      }])))
      return
    }
    let live = true
    const aborts = new Map<string, AbortController>()
    const read = (machineID: string) => {
      aborts.get(machineID)?.abort()
      const abort = new AbortController()
      aborts.set(machineID, abort)
      setReadings((before) => before[machineID]?.phase === "settled" ? before :
        ({ ...before, [machineID]: { phase: "loading" } }))
      void source.readMachine(machineID, abort.signal).then((value) => {
        if (!live || abort.signal.aborted) return
        setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
          value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined, value) } }))
      }, (error: unknown) => {
        if (!live || abort.signal.aborted) return
        setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
          value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
            { kind: "unavailable", reason: problemOf(error) }) } }))
      })
    }
    const stop = source.subscribe(({ machineID, sessionID, kind }) => {
      if (!machines.some((machine) => machine.id === machineID)) return
      if (kind === "access_changed" || kind === "detail_changed" && sessionID) {
        // Keep the verified label visible while its replacement is read. The
        // presentation key includes the execution generation, so a restarted
        // Session cannot reuse this label for a different execution.
        presentationPasses.current.delete(machineID)
        presentationFailures.current.delete(machineID)
        refreshPresentation((revision) => revision + 1)
        return
      }
      read(machineID)
    })
    for (const machine of machines) read(machine.id)
    return () => { live = false; stop(); for (const abort of aborts.values()) abort.abort() }
  }, [source, machines.map((machine) => machine.id + ":" + machine.freshness).join("\0")])

  useEffect(() => {
    if (!source) return
    const deadlines = Object.entries(readings).flatMap(([machineID, reading]) => {
      const at = reading.phase === "settled" ? projectionRefreshAt(reading.value, STATUS_FRESH_MS) : null
      return at === null ? [] : [{ machineID, at }]
    })
    if (!deadlines.length) return
    const earliest = Math.min(...deadlines.map(({ at }) => at))
    const abort = new AbortController()
    const timer = window.setTimeout(() => {
      for (const { machineID, at } of deadlines) {
        if (at !== earliest || !machines.some((machine) => machine.id === machineID)) continue
        void source.readMachine(machineID, abort.signal).then((value) => {
          if (!abort.signal.aborted) setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
            value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined, value) } }))
        }, (error: unknown) => {
          if (!abort.signal.aborted) setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
            value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
              { kind: "unavailable", reason: problemOf(error) }) } }))
        })
      }
    }, Math.max(1, earliest - Date.now()))
    return () => { window.clearTimeout(timer); abort.abort() }
  }, [readings, source, machines.map((machine) => machine.id).join("\0")])

  const presentationMachines = machines.flatMap((machine) => {
    const reading = readings[machine.id]
    return reading?.phase === "settled" && reading.value.kind === "ready" &&
      reading.value.rows.some((row) => row.freshness === "current")
      ? [{ machineID: machine.id, pass: presentationPass(reading.value) }] : []
  })
  const passesKey = presentationMachines.map(({ machineID, pass }) => machineID + ":" + pass).join("\0")
  useEffect(() => {
    if (!source?.readMachinePresentations) return
    const currentKeys = new Set(machines.flatMap((machine) => {
      const reading = readings[machine.id]
      return reading?.phase === "settled" && reading.value.kind === "ready"
        ? reading.value.rows.map((row) => destinationKey(row.destination)) : []
    }))
    for (const [key, value] of presentations.current) {
      if (!currentKeys.has(key))
        presentations.current.delete(key)
    }
    const pending = presentationMachines.filter(({ machineID, pass }) =>
      presentationPasses.current.get(machineID) !== pass)
    if (!pending.length) return
    const abort = new AbortController()
    for (const { machineID, pass } of pending) presentationPasses.current.set(machineID, pass)
    for (const { machineID } of pending) presentationFailures.current.delete(machineID)
    const inFlight = new Set(pending.map(({ machineID }) => machineID))
    let next = 0
    const worker = async () => {
      while (!abort.signal.aborted && next < pending.length) {
        const { machineID, pass } = pending[next++]
        const rows = await source.readMachinePresentations!(machineID, abort.signal).catch(() => null)
        if (abort.signal.aborted) return
        inFlight.delete(machineID)
        if (rows && presentationPasses.current.get(machineID) === pass) {
          for (const row of rows) {
            const key = destinationKey(row.destination)
            const previous = presentations.current.get(key)
            presentations.current.set(key, {
              machineID, sessionID: row.destination.sessionID, pass, title: row.title,
              icon: row.icon ?? previous?.icon, cwd: row.cwd ?? previous?.cwd,
              status: row.status ?? previous?.status,
            })
          }
          const expected = readings[machineID]
          const missing = expected?.phase === "settled" && expected.value.kind === "ready" &&
            expected.value.rows.some((row) => row.freshness === "current" &&
              !presentations.current.has(destinationKey(row.destination)))
          if (missing) presentationFailures.current.set(machineID, pass)
          redraw((revision) => revision + 1)
        } else if (presentationPasses.current.get(machineID) === pass) {
          presentationFailures.current.set(machineID, pass)
          redraw((revision) => revision + 1)
        }
      }
    }
    for (let i = 0; i < Math.min(4, pending.length); i++) void worker()
    return () => {
      abort.abort()
      for (const { machineID, pass } of pending.filter(({ machineID }) => inFlight.has(machineID))) {
        if (presentationPasses.current.get(machineID) === pass) presentationPasses.current.delete(machineID)
      }
    }
  }, [source, passesKey, presentationRevision])

  const search = filter.trim().toLocaleLowerCase()
  const groups = machines.map((machine) => {
    const reading = readings[machine.id]
    const pass = reading?.phase === "settled" && reading.value.kind === "ready"
      ? reading.value.snapshotGeneration ?? String(reading.value.observedAt) : ""
    const missingTitle = reading?.phase === "settled" && reading.value.kind === "ready" &&
      reading.value.rows.some((row) => row.freshness === "current" &&
        !presentations.current.has(destinationKey(row.destination)))
    const presentationFailed = !!missingTitle && presentationFailures.current.get(machine.id) === pass
    const presentationLoading = !!missingTitle && !presentationFailed
    const filtered = reading?.phase === "settled" && reading.value.rows
      ? reading.value.rows.filter((row) => matchesFleetFilter(row, statusFilter) &&
        (!search || `${machine.name} ${machine.id} ${machine.platform} ${presentations.current.get(destinationKey(row.destination))?.title ?? ""} ${row.destination.sessionID} ${stateWord(row.state)}`
          .toLocaleLowerCase().includes(search))) : []
    const shown = filtered.map((row) => {
      const presentation = presentations.current.get(destinationKey(row.destination))
      // A new ss/ pass can arrive before the matching machine list read.
      // Keep the last display state for this exact execution while it refreshes.
      const status = displayedPresentationStatus(row, presentation)
      return status?.state ? { ...row, state: status.state } : row
    })
    const waitingKey = fleetWaitingKey(shown)
    const titles = new Map(shown.map((row) => [row.destination.sessionID,
      presentations.current.get(destinationKey(row.destination))?.title ?? ""]))
    const hold = orderHolds.current.get(machine.id)
    if (hold && hold.waiting !== waitingKey)
      orderHolds.current.delete(machine.id)
    const rows = arrangeFleetRows(shown, titles, orderHolds.current.get(machine.id) ?? null)
    return { machine, reading, rows, waitingKey, presentationFailed, presentationLoading }
  }).filter(({ machine, rows }) => (statusFilter === "all" || rows.length > 0) && (!search || rows.length > 0 ||
    `${machine.name} ${machine.id} ${machine.platform}`.toLocaleLowerCase().includes(search)))
  useEffect(() => {
    if (swiped && !groups.some(({ machine, rows }) => !collapsed[machine.id] && machine.freshness === "current" &&
      rows.some(({ row }) => destinationKey(row.destination) === swiped && row.freshness === "current"))) swipe.closeOpen()
  }, [swiped, groups.map(({ machine, rows }) => machine.id + ":" + machine.freshness + ":" + !!collapsed[machine.id] + ":" +
    rows.map(({ row }) => destinationKey(row.destination) + ":" + row.freshness).join(",")).join("|")])
  const freezeOrder = () => {
    for (const { machine, rows, waitingKey } of groups) {
      if (orderHolds.current.has(machine.id)) continue
      orderHolds.current.set(machine.id, {
        order: rows.map(({ row }) => row.destination.sessionID),
        waiting: waitingKey,
      })
    }
  }
  const thawOrder = () => {
    if (!orderHolds.current.size) return
    orderHolds.current.clear()
    redrawOrder((revision) => revision + 1)
  }
  const chooseAction = (action: MachineToolbarAction) => {
    if (!onMachineAction || machines.length === 0) return
    if (machines.length === 1) onMachineAction(machines[0].id, action)
    else setMachineAction(action)
  }
  const openScheduled = async (sessionID: string, machineID?: string) => {
    if (!source) return
    const candidates = machineID ? [machineID] : machines.map((machine) => machine.id)
    const matches = candidates.flatMap((id) => {
      const reading = readings[id]
      return reading?.phase === "settled" && reading.value.kind === "ready"
        ? reading.value.rows.filter((row) => row.destination.sessionID === sessionID && row.freshness === "current") : []
    })
    if (matches.length !== 1) return
    const destination = matches[0].destination
    const fresh = await source.readMachine(destination.machineID, new AbortController().signal).catch(() => null)
    if (fresh?.kind === "ready" && fresh.rows.some((row) => destinationKey(row.destination) === destinationKey(destination) &&
      row.freshness === "current")) onOpen(destination)
  }
  const canOpenScheduled = (sessionID: string, machineID?: string) => {
    const candidates = machineID ? [machineID] : machines.map((machine) => machine.id)
    return candidates.flatMap((id) => {
      const reading = readings[id]
      return reading?.phase === "settled" && reading.value.kind === "ready" &&
        reading.value.rows.filter((row) => row.destination.sessionID === sessionID && row.freshness === "current") || []
    }).length === 1
  }

  return <>
    <SessionToolbar inputRef={searchField} filter={filter} onFilter={onFilter} terminalMode={false}
      placeholder={nextWord("cloudAllSearch")} label={nextWord("cloudAllSearch")}
      onSessions={() => { location.hash = "#all-machines" }}
      onTerminals={() => chooseAction("terminal")}
      onVoice={() => chooseAction("voice")} onWork={() => chooseAction("work")}
      onStart={() => chooseAction("start")} />
    <div className="scroller list-scroll cloud-all-list-scroll" id="list-scroll" ref={listScroll}
      onMouseEnter={freezeOrder} onMouseLeave={thawOrder} onTouchStart={freezeOrder}
      onTouchEnd={() => window.setTimeout(() => { if (swipe.openId() === null) thawOrder() }, 1200)}>
      <div className="cloud-all-status-filters" role="group" aria-label={nextWord("cloudAllState")}>
        <button type="button" className="cloud-all-status-filter" aria-pressed={statusFilter === "attention"}
          onClick={() => setStatusFilter((value) => value === "attention" ? "all" : "attention")}>{nextWord("cloudAllAttention")}</button>
        <button type="button" className="cloud-all-status-filter" aria-pressed={statusFilter === "working"}
          onClick={() => setStatusFilter((value) => value === "working" ? "all" : "working")}>{nextWord("cloudAllStateWorking")}</button>
        <button type="button" className="cloud-all-status-filter" aria-pressed={statusFilter === "idle"}
          onClick={() => setStatusFilter((value) => value === "idle" ? "all" : "idle")}>{nextWord("cloudAllFilterIdle")}</button>
      </div>
      <div className="cloud-all-groups">
        {groups.map(({ machine, reading, rows, presentationFailed, presentationLoading }) => <section key={machine.id} className="cloud-all-group"
          aria-label={`${machine.name} ${machine.id}`}>
          <h2 className="cloud-all-group-heading">
            <button type="button" aria-expanded={!collapsed[machine.id]}
              aria-label={nextWord(collapsed[machine.id] ? "cloudAllExpandMachine" : "cloudAllCollapseMachine", { machine: machine.name })}
              onClick={() => setCollapsed((before) => ({ ...before, [machine.id]: !before[machine.id] }))}>
              <span className="cloud-all-group-name">{machine.name} <small>{machine.platform}</small>
                {rows.length > 0 && <span className="cloud-all-group-count"
                  aria-label={nextWord("cloudAllSessionCount", { count: rows.length })}>{rows.length}</span>}
              </span>
              <svg aria-hidden="true" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6" /></svg>
            </button></h2>
          {!collapsed[machine.id] && <>
          {machine.freshness !== "current" && reading?.phase === "settled" && reading.value.kind === "ready" &&
            <p role="status">{machine.freshness === "stale" ? nextWord("cloudAllStaleSource") : nextWord("cloudAllUnknownSource")}</p>}
          {!reading || reading.phase === "loading" ? <p role="status">{nextWord("cloudAllLoading")}</p> : <>
            {reading.value.kind === "unavailable" && <p role="status">{problemWord(reading.value.reason)}</p>}
            {presentationLoading && <p role="status">{nextWord("cloudAllLoading")}</p>}
            {presentationFailed && <p role="alert">{nextWord("cloudAllNameUnavailable")} <button type="button"
              onClick={() => { presentationPasses.current.delete(machine.id); presentationFailures.current.delete(machine.id)
                refreshPresentation((revision) => revision + 1) }}>{nextWord("cloudAllTryAgain")}</button></p>}
            {presentationLoading || presentationFailed ? null : rows.length === 0 ? reading.value.unknownTargets ? null : reading.value.kind === "ready" ?
              <p>{reading.value.rows.length === 0 ? nextWord("cloudAllEmptyMachine") : nextWord("cloudAllNoMatch")}</p> : null
              : <ul className="rows" role="listbox" aria-label={`${machine.name} ${nextWord("cloudSingleSessions")}`}>
                {rows.map(({ row, depth, branchThrough, ancestorThrough }) => {
                  const presentation = presentations.current.get(destinationKey(row.destination))
                  return <ProjectedRow key={destinationKey(row.destination)}
                    title={presentation?.title || row.destination.sessionID} icon={presentation?.icon} cwd={presentation?.cwd}
                    status={displayedPresentationStatus(row, presentation)}
                    sessionID={row.destination.sessionID} machineName={machine.name} platform={machine.platform}
                    assistant={row.assistant} backend={row.backend} state={row.state} stateLabel={stateWord(row.state)}
                    freshness={row.freshness === "current" ? "" : nextWord(
                      row.freshness === "stale" ? "cloudAllStale" : "cloudAllUnknown")}
                    lastMovementAt={row.lastMovementAt} attention={row.needsAttention === true}
                    depth={depth} branchThrough={branchThrough} ancestorThrough={ancestorThrough}
                    open={!!target && destinationKey(target) === destinationKey(row.destination)}
                    selectionKey={destinationKey(row.destination)}
                    swiped={swiped === destinationKey(row.destination)}
                    consumePress={() => swipe.tookThePress()}
                    onClose={row.freshness === "current" && machine.freshness === "current"
                      ? () => { swipe.closeOpen(); thawOrder(); onCloseRequest(row.destination, presentation?.title || row.destination.sessionID) } : undefined}
                    onOpen={() => onOpen(row.destination)} />
                })}</ul>}
          </>}
          {reading?.phase === "settled" && !!reading.value.unknownTargets &&
            <p role="status">{nextWord("cloudAllUnknownTargets", { count: reading.value.unknownTargets })}</p>}
          </>}
        </section>)}
      </div>
      {groups.length === 0 && <p>{nextWord("cloudAllNoMatch")}</p>}
      <ScheduleSection arrived={true} onOpen={openScheduled} canOpen={canOpenScheduled} />
    </div>
    {/* Which machine, asked once for an action that needs one. Starting asks
        where to start and leaves this list where it is; the others move the
        console onto the machine, which is what their sheets read. */}
    <dialog ref={machineDialog} className="cloud-all-machine-dialog"
      aria-label={nextWord(machineAction === "start" ? "cloudStartWhere" : "cloudSwitch")}
      onCancel={(event) => { event.preventDefault(); setMachineAction(null) }}>
      <h2>{nextWord(machineAction === "start" ? "cloudStartWhere" : "cloudSwitch")}</h2>
      {machines.map((machine) => <button key={machine.id} type="button" onClick={() => {
        const action = machineAction
        setMachineAction(null)
        if (action) onMachineAction?.(machine.id, action)
      }}>{machine.name} · {machine.platform}</button>)}
      <button type="button" onClick={() => setMachineAction(null)}>{nextWord("cloudActionCancel")}</button>
    </dialog>
  </>
}

function problemOf(error: unknown): ProjectionProblem {
  const code = (error as { code?: unknown } | null)?.code
  return code === "offline" || code === "stale" || code === "old_version" || code === "no_permission" || code === "event_gap"
    ? code : "unresponsive"
}

function problemWord(reason: ProjectionProblem): string {
  return nextWord(({ offline: "cloudAllOffline", stale: "cloudAllStaleSource", unknown: "cloudAllUnknownSource",
    old_version: "cloudAllOldVersion", no_permission: "cloudAllNoPermission", unresponsive: "cloudAllUnresponsive",
    event_gap: "cloudAllEventGap", bad_projection: "cloudAllBadProjection" })[reason] as "cloudAllOffline")
}

function stateWord(state: string): string {
  switch (state) {
    case "working": return nextWord("cloudAllStateWorking")
    case "waiting": return nextWord("cloudAllStateWaiting")
    case "idle": return nextWord("cloudAllStateIdle")
    default: return nextWord("cloudAllUnknown")
  }
}
