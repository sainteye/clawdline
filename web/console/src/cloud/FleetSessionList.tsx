import { SessionStateEmpty } from "../session/SessionStateEmpty.js"
import { createPortal } from "react-dom"
import { SessionStateFilterControl } from "../session/SessionStateFilter.js"
import { stateFilterCounts } from "../session/state-filter.js"
import { totalSessionWords } from "../session-reading.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import * as L from "../legacy/bridge.js"
import { ProjectedRow } from "../session/List.js"
import { onAttentionRead } from "../session/attention-changed.js"
import { Swipes, sessionSwipeActions } from "../session/swipe.js"
import { usePullToRefresh } from "../session/use-pull-to-refresh.js"
import { useSwipeToEnd } from "../session/use-swipe.js"
import type { OrderHold } from "../session/order.js"
import { SessionToolbar } from "../session/SessionToolbar.js"
import { ScheduleSection, scheduleMachineReady } from "../pages/schedules.js"
import { destinationKey, displayedPresentationStatus, fleetRowTitle, presentationPass, projectionRefreshAt, settleProjection,
  shownDestinationKeys, mergeListPresentation,
  type MachineSessionProjection, type ProjectionProblem, type SessionDestination,
  type SessionListPresentation, type SessionProjectionSource, type FleetMachine,
  type ProjectedSession } from "./all-machine-sessions.js"
import { STATUS_FRESH_MS } from "./status-projection.js"
import { STATUS_GAP_RETRY_MS } from "./status-source.js"
import { arrangeFleetRows, fleetWaitingKey } from "./fleet-order.js"
import { matchesFleetFilter, type FleetFilter } from "./fleet-filters.js"
import { CarrierDot } from "./CarrierDot.js"
import { machineLight, type CarrierReading } from "./carrier-state.js"
import { machineSeenWord } from "./machine-seen.js"
import { MachinePlatformIcon } from "./MachinePlatformIcon.js"

export type MachineToolbarAction = "terminal" | "voice" | "work" | "start" | "start_terminal"
type Reading = { phase: "loading" } | { phase: "settled"; value: MachineSessionProjection }

/** Only the fleet list is new; the conversation and composer belong to SessionsPage. */
export function FleetSessionList({ machines, source, target, filter, onFilter, onOpen,
  carrierOf, onConnection, onMachineAction, onCloseRequest, onArchiveRequest, onPresentations, onProjection, titleRevision, confirmedTitles }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  titleRevision?: number
  confirmedTitles?: ReadonlyMap<string, string>
  /** Which road this page's reads of that machine take, read as the row is drawn. */
  carrierOf?: (machineID: string) => CarrierReading | null
  target: SessionDestination | null
  filter: string
  onFilter: (value: string) => void
  onOpen: (target: SessionDestination) => void
  onPresentations?: (machineID: string, rows: readonly (SessionListPresentation & {
    destination: SessionDestination; projection?: ProjectedSession })[]) => void
  onProjection?: (machineID: string, reading: MachineSessionProjection) => void
  onConnection: (machineID: string) => void
  onMachineAction?: (machineID: string, action: MachineToolbarAction) => void
  onCloseRequest: (target: SessionDestination, title: string) => void
  onArchiveRequest: (target: SessionDestination, title: string) => void
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const [filterSlot, setFilterSlot] = useState<HTMLElement | null>(null)
  useEffect(() => { setFilterSlot(document.getElementById("session-state-filter-slot")) }, [])
  useEffect(() => {
    for (const machine of machines) {
      const reading = readings[machine.id]
      if (reading?.phase === "settled") onProjection?.(machine.id, reading.value)
    }
  }, [readings, machines, onProjection])
  const presentations = useRef(new Map<string, { machineID: string; sessionID: string; pass: string } & SessionListPresentation>())
  const presentationPasses = useRef(new Map<string, string>())
  const presentationFailures = useRef(new Map<string, string>())
  const [presentationRevision, refreshPresentation] = useState(0)
  const [manualRefresh, setManualRefresh] = useState(0)
  const [refreshFeedback, setRefreshFeedback] = useState<"idle" | "loading" | "done" | "failed">("idle")
  const refreshStartedAt = useRef(0)
  const refreshCompletion = useRef<{ promise: Promise<void>; resolve: () => void } | null>(null)
  useEffect(() => {
    if (refreshFeedback === "loading") return
    refreshCompletion.current?.resolve()
    refreshCompletion.current = null
  }, [refreshFeedback])
  useEffect(() => () => { refreshCompletion.current?.resolve() }, [])
  const previousTarget = useRef(target)
  useEffect(() => {
    if (!target || !source?.refreshSessionStatus) return
    const abort = new AbortController()
    // The published ss/ row reaches the existing subscription and updates both
    // the list and open detail. A refused or interrupted read leaves them as is.
    void source.refreshSessionStatus(target, abort.signal).catch(() => undefined)
    return () => abort.abort()
  }, [source, target && destinationKey(target)])
  useEffect(() => {
    if (!target || !source?.refreshSessionStatus) return
    return onAttentionRead((sessionID) => {
      if (sessionID !== target.sessionID) return
      void source.refreshSessionStatus!(target, new AbortController().signal).catch(() => undefined)
    })
  }, [source, target && destinationKey(target)])
  const refreshList = () => {
    presentationPasses.current.clear()
    presentationFailures.current.clear()
    setManualRefresh((value) => value + 1)
    refreshPresentation((value) => value + 1)
  }
  useEffect(() => {
    if (titleRevision) refreshList()
  }, [titleRevision])
  const refreshFromButton = () => {
    if (refreshCompletion.current) return refreshCompletion.current.promise
    if (!source) { setRefreshFeedback("failed"); return }
    let resolve!: () => void
    const promise = new Promise<void>((done) => { resolve = done })
    refreshCompletion.current = { promise, resolve }
    refreshStartedAt.current = Date.now()
    setRefreshFeedback("loading")
    refreshList()
    return promise
  }
  useEffect(() => {
    if (refreshFeedback !== "done" && refreshFeedback !== "failed") return
    const timer = window.setTimeout(() => setRefreshFeedback("idle"), 1800)
    return () => window.clearTimeout(timer)
  }, [refreshFeedback])
  useEffect(() => {
    if (previousTarget.current && !target) refreshList()
    previousTarget.current = target
  }, [target])
  const [, redraw] = useState(0)
  const [machineAction, setMachineAction] = useState<MachineToolbarAction | null>(null)
  const [statusFilter, setStatusFilter] = useState<FleetFilter>("all")
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const machineDialog = useRef<HTMLDialogElement | null>(null)
  const searchField = useRef<HTMLInputElement | null>(null)
  const listScroll = useRef<HTMLDivElement | null>(null)
  const swipe = useRef(new Swipes()).current
  const swiped = useSwipeToEnd(listScroll, swipe)
  const pullPad = useRef<HTMLDivElement | null>(null)
  const pullWord = usePullToRefresh(listScroll, pullPad, refreshFromButton, swipe)
  const orderHolds = useRef(new Map<string, OrderHold>())
  const [, redrawOrder] = useState(0)

  // The original Session list hands all its canvases to one page clock after
  // every render. Fleet rows use the same clock, including after status updates.
  useEffect(() => {
    L.registerSpinners([...(listScroll.current?.querySelectorAll<HTMLCanvasElement>('.cloud-all-group-body[data-open="true"] canvas.spin') ?? [])])
  })

  useEffect(() => {
    if (machineAction) showModalWithoutChoice(machineDialog.current)
    else machineDialog.current?.close()
  }, [machineAction])

  useEffect(() => {
    if (!source) {
      if (refreshFeedback === "loading") setRefreshFeedback("failed")
      setReadings(Object.fromEntries(machines.map((machine) => [machine.id, {
        phase: "settled", value: { kind: "unavailable", reason: "old_version" },
      }])))
      return
    }
    let live = true
    let resultTimer: number | undefined
    const aborts = new Map<string, AbortController>()
    const pendingRefresh = new Set(refreshFeedback === "loading" ? machines.map((machine) => machine.id) : [])
    const showResult = () => {
      resultTimer = window.setTimeout(() => setRefreshFeedback("done"),
        Math.max(0, 750 - (Date.now() - refreshStartedAt.current)))
    }
    if (refreshFeedback === "loading" && pendingRefresh.size === 0) showResult()
    const finishRefresh = (machineID: string) => {
      if (!pendingRefresh.delete(machineID)) return
      if (pendingRefresh.size === 0) showResult()
    }
    const read = (machineID: string) => {
      aborts.get(machineID)?.abort()
      const abort = new AbortController()
      aborts.set(machineID, abort)
      setReadings((before) => before[machineID]?.phase === "settled" ? before :
        ({ ...before, [machineID]: { phase: "loading" } }))
      void source.readMachine(machineID, abort.signal).then((value) => {
        if (!live || abort.signal.aborted) return
        finishRefresh(machineID)
        if (value.kind === "ready") scheduleMachineReady(machineID)
        setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
          value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
            value, STATUS_GAP_RETRY_MS) } }))
      }, (error: unknown) => {
        if (!live || abort.signal.aborted) return
        finishRefresh(machineID)
        setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
          value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
            { kind: "unavailable", reason: problemOf(error) }, STATUS_GAP_RETRY_MS) } }))
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
        if (kind === "access_changed") read(machineID)
        return
      }
      read(machineID)
    })
    for (const machine of machines) read(machine.id)
    return () => {
      live = false
      if (resultTimer !== undefined) window.clearTimeout(resultTimer)
      stop()
      for (const abort of aborts.values()) abort.abort()
    }
  }, [source, manualRefresh, machines.map((machine) => machine.id + ":" + machine.freshness).join("\0")])

  useEffect(() => {
    if (!source) return
    const deadlines = Object.entries(readings).flatMap(([machineID, reading]) => {
      const at = reading.phase === "settled" ? projectionRefreshAt(reading.value, STATUS_FRESH_MS,
        STATUS_GAP_RETRY_MS) : null
      return at === null ? [] : [{ machineID, at }]
    })
    if (!deadlines.length) return
    const earliest = Math.min(...deadlines.map(({ at }) => at))
    const abort = new AbortController()
    const timer = window.setTimeout(() => {
      for (const { machineID, at } of deadlines) {
        if (at !== earliest || !machines.some((machine) => machine.id === machineID)) continue
        void source.readMachine(machineID, abort.signal).then((value) => {
          if (!abort.signal.aborted) {
            if (value.kind === "ready") scheduleMachineReady(machineID)
            setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
              value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
                value, STATUS_GAP_RETRY_MS) } }))
          }
        }, (error: unknown) => {
          if (!abort.signal.aborted) setReadings((before) => ({ ...before, [machineID]: { phase: "settled",
            value: settleProjection(machineID, before[machineID]?.phase === "settled" ? before[machineID].value : undefined,
              { kind: "unavailable", reason: problemOf(error) }, STATUS_GAP_RETRY_MS) } }))
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
    const currentKeys = shownDestinationKeys(machines.map((machine) => {
      const reading = readings[machine.id]
      return reading?.phase === "settled" ? reading.value : undefined
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
              machineID, sessionID: row.destination.sessionID, pass,
              ...mergeListPresentation(row, previous),
            })
          }
          const expected = readings[machineID]
          const projected = new Map(expected?.phase === "settled" && expected.value.kind === "ready"
            ? expected.value.rows.map((row) => [destinationKey(row.destination), row] as const) : [])
          onPresentations?.(machineID, rows.map((row) => ({ ...row,
            projection: projected.get(destinationKey(row.destination)) })))
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
  }, [source, passesKey, presentationRevision, onPresentations])

  const filterRows = machines.flatMap((machine) => {
    const reading = readings[machine.id]
    return reading?.phase === "settled" ? reading.value.rows ?? [] : []
  })
  const filterIncomplete = machines.some((machine) => {
    const reading = readings[machine.id]
    return !reading || reading.phase !== "settled" || reading.value.kind !== "ready"
  })
  const filterCounts = stateFilterCounts(filterRows, (row, value) => matchesFleetFilter(row, value,
    displayedPresentationStatus(row, presentations.current.get(destinationKey(row.destination)))))

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
      ? reading.value.rows.filter((row) => matchesFleetFilter(row, statusFilter, displayedPresentationStatus(row, presentations.current.get(destinationKey(row.destination)))) &&
        (!search || `${machine.name} ${machine.id} ${machine.platform} ${row.machineScope ? "Clawdfather" : (confirmedTitles?.get(destinationKey(row.destination)) ?? presentations.current.get(destinationKey(row.destination))?.title ?? "")} ${row.destination.sessionID} ${stateWord(row.state)}`
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
      confirmedTitles?.get(destinationKey(row.destination)) ?? presentations.current.get(destinationKey(row.destination))?.title ?? ""]))
    const hold = orderHolds.current.get(machine.id)
    if (hold && hold.waiting !== waitingKey)
      orderHolds.current.delete(machine.id)
    const rows = arrangeFleetRows(shown, titles, orderHolds.current.get(machine.id) ?? null)
    return { machine, reading, rows, waitingKey, presentationFailed, presentationLoading }
  }).filter(({ machine, rows }) => (statusFilter === "all" || rows.length > 0) && (!search || rows.length > 0 ||
    `${machine.name} ${machine.id} ${machine.platform}`.toLocaleLowerCase().includes(search)))
  useEffect(() => {
    if (swiped && !groups.some(({ machine, reading, rows }) => !(collapsed[machine.id] ?? emptyMachine(reading)) && machine.freshness === "current" &&
      rows.some(({ row }) => destinationKey(row.destination) === swiped && row.freshness === "current"))) swipe.closeOpen()
  }, [swiped, groups.map(({ machine, reading, rows }) => machine.id + ":" + machine.freshness + ":" + (collapsed[machine.id] ?? emptyMachine(reading)) + ":" +
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
      {filterSlot && createPortal(<SessionStateFilterControl value={statusFilter} onChange={setStatusFilter}
        counts={filterCounts} incomplete={filterIncomplete} onRefresh={refreshFromButton} refreshState={refreshFeedback}
        summary={filterIncomplete ? null : <span id="counts">{filterCounts.working
          ? L.fillString(L.strings.webCountWorking, { n: filterCounts.working }) : totalSessionWords(filterCounts.all)}</span>} />, filterSlot)}
      <div className="ptr" id="ptr" ref={pullPad}>
        <span id="ptr-label">{pullWord === "release" ? L.strings.webPullRelease :
          pullWord === "busy" ? L.strings.webPullBusy : L.strings.webPull}</span>
      </div>
      <div className="cloud-all-groups">
        {groups.map(({ machine, reading, rows, presentationFailed, presentationLoading }) => {
          const carrier = machineLight(machine.freshness, carrierOf?.(machine.id) ?? null)
          // The roster has heard nothing from it inside its window: say that, and when,
          // instead of the reading's own reason, which for a machine that is off is a
          // retry that never ends ("reading this machine again").
          const offline = machine.freshness === "stale"
          const isCollapsed = collapsed[machine.id] ?? emptyMachine(reading)
          return <section key={machine.id} className="cloud-all-group"
          aria-label={`${machine.name} ${machine.id}` + (carrier ? ` · ${nextWord(carrier.word, { machine: machine.name })}` : "")}>
          <h2 className="cloud-all-group-heading">
            <button type="button" className="cloud-all-connection"
              aria-label={`${machine.name} · ${nextWord("msTabConnection")}`}
              aria-haspopup="dialog" onClick={() => onConnection(machine.id)}>
              <span className="cloud-all-group-name">{carrier && <CarrierDot reading={carrier} machine={machine.name} />}
                {machine.name} <small>{machine.platform}</small>
                {rows.length > 0 && <span className="cloud-all-group-count"
                  aria-label={nextWord("cloudAllSessionCount", { count: rows.length })}>{rows.length}</span>}
              </span>
            </button>
            <button type="button" className="cloud-all-collapse" aria-expanded={!isCollapsed}
              aria-label={nextWord(isCollapsed ? "cloudAllExpandMachine" : "cloudAllCollapseMachine", { machine: machine.name })}
              onClick={() => setCollapsed((before) => ({ ...before, [machine.id]: !isCollapsed }))}>
              <svg aria-hidden="true" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6" /></svg>
            </button></h2>
          <div className="cloud-all-group-body" data-open={!isCollapsed}
            inert={isCollapsed} aria-hidden={isCollapsed}>
          <div className="cloud-all-group-content">
          {/* A verified Session reading can settle before the roster's machine
              freshness changes. Its own completeness and row freshness are the
              authority here; an unrelated roster state must not leave a
              permanent "checking connection" banner above settled results. */}
          {offline && <p role="status" className="cloud-all-offline">{nextWord("cloudAllMachineOffline",
            { seen: machineSeenWord(machine.seenAt ?? null) })}</p>}
          {!reading || reading.phase === "loading" ? !offline && <LoadingSkeleton id="SK-02" label={nextWord("cloudAllLoading")} /> : <>
            {/* // refusal-ok: Background verification keeps its reason and scheduled retry without announcing transient recovery above retained rows. */}
            {reading.value.kind === "unavailable" && !offline && (reading.value.reason === "unknown" ||
              reading.value.reason === "stale" || reading.value.reason === "event_gap"
              ? !reading.value.rows?.length && <p role="status">{nextWord("cloudAllLoading")}</p>
              : <p role="status">{problemWord(reading.value.reason)}</p>)}
            {reading.value.kind === "ready" && !reading.value.complete && !offline &&
              <p role="status">{nextWord("cloudAllPartialMachine")}</p>}
            {presentationLoading && <LoadingSkeleton id="SK-02" label={nextWord("cloudAllLoading")} />}
            {presentationFailed && <p role="alert">{nextWord("cloudAllNameUnavailable")} <button type="button"
              onClick={() => { presentationPasses.current.delete(machine.id); presentationFailures.current.delete(machine.id)
                refreshPresentation((revision) => revision + 1) }}>{nextWord("cloudAllTryAgain")}</button></p>}
            {presentationLoading || presentationFailed ? null : rows.length === 0 ? reading.value.unknownTargets ? null : reading.value.kind === "ready" ?
              <p>{reading.value.rows.length === 0 ? nextWord(reading.value.complete ? "cloudAllEmptyMachine" : "cloudAllPartialEmpty") : nextWord("cloudAllNoMatch")}</p> : null
              : <ul className="rows" role="listbox" aria-label={`${machine.name} ${nextWord("cloudSingleSessions")}`}>
                {rows.map(({ row, depth, branchThrough, ancestorThrough }) => {
                  const presentation = presentations.current.get(destinationKey(row.destination))
                  const title = fleetRowTitle(row, presentation, nextWord("cloudAllUnknown"),
                    confirmedTitles?.get(destinationKey(row.destination)))
                  const actions = sessionSwipeActions(row.freshness,
                    displayedPresentationStatus(row, presentation)?.sessionId)
                  return <ProjectedRow key={destinationKey(row.destination)}
                    title={title} icon={presentation?.icon} cwd={presentation?.cwd}
                    machineScope={row.machineScope === true} coordinatorBound={row.coordinatorBound === true}
                    status={displayedPresentationStatus(row, presentation)}
                    sessionID={row.destination.sessionID} machineID={machine.id} machineName={machine.name}
                    assistant={row.assistant ?? presentation?.assistant} state={row.state} stateLabel={stateWord(row.state)}
                    freshness={row.freshness === "unknown" ? nextWord("cloudAllUnknown") : ""}
                    lastMovementAt={row.lastMovementAt} attention={row.needsAttention === true}
                    depth={depth} branchThrough={branchThrough} ancestorThrough={ancestorThrough}
                    open={!!target && destinationKey(target) === destinationKey(row.destination)}
                    selectionKey={destinationKey(row.destination)}
                    swiped={swiped === destinationKey(row.destination)}
                    consumePress={() => swipe.tookRowPress(destinationKey(row.destination))}
                    onClose={actions.close
                      ? () => { swipe.closeOpen(); thawOrder(); onCloseRequest(row.destination,
                        title) } : undefined}
                    onArchive={actions.archive
                      ? () => { swipe.closeOpen(); thawOrder(); onArchiveRequest(row.destination,
                        title) } : undefined}
                    onOpen={() => onOpen(row.destination)} />
                })}</ul>}
          </>}
          {reading?.phase === "settled" && !!reading.value.unknownTargets &&
            <p role="status">{nextWord("cloudAllUnknownTargets", { count: reading.value.unknownTargets })}</p>}
          </div></div>
        </section>
        })}
      </div>
      {groups.length === 0 && (statusFilter !== "all"
        ? <SessionStateEmpty filter={statusFilter} incomplete={filterIncomplete} onReset={() => setStatusFilter("all")} />
        : <p>{nextWord("cloudAllNoMatch")}</p>)}
      <ScheduleSection arrived={true} waitForFleet onOpen={openScheduled} canOpen={canOpenScheduled} />
    </div>
    {/* Which machine, asked once for an action that needs one. Starting asks
        where to start and leaves this list where it is; the others move the
        console onto the machine, which is what their sheets read. */}
    <dialog ref={machineDialog} className="cloud-all-machine-dialog"
      aria-label={nextWord(machineAction === "start" ? "cloudStartWhere" : "cloudSwitch")}
      onCancel={(event) => { event.preventDefault(); setMachineAction(null) }}
      onClick={(event) => {
        if (event.target !== event.currentTarget) return
        const { left, right, top, bottom } = event.currentTarget.getBoundingClientRect()
        if (event.clientX < left || event.clientX > right || event.clientY < top || event.clientY > bottom) {
          setMachineAction(null)
        }
      }}>
      <h2>{nextWord(machineAction === "start" ? "cloudStartWhere" : "cloudSwitch")}</h2>
      <p className="cloud-all-machine-help">{nextWord(machineAction === "start" ? "cloudStartWhereHelp" : "cloudSwitchWhereHelp")}</p>
      <div className="cloud-all-machine-options">{machines.map((machine) => <button className="cloud-all-machine-option" key={machine.id} type="button" onClick={() => {
        const action = machineAction
        setMachineAction(null)
        if (action) onMachineAction?.(machine.id, action)
      }}>
        <MachinePlatformIcon platform={machine.platform} />
        <span className="cloud-all-machine-copy"><strong>{machine.name}</strong><small>{machine.platform}</small></span>
        <svg className="cloud-all-machine-arrow" aria-hidden="true" viewBox="0 0 24 24" fill="none"
          stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
          <path d="m9 5 7 7-7 7" />
        </svg>
      </button>)}</div>
      <div className="cloud-all-machine-actions">
        <button className="cloud-all-machine-cancel" type="button" onClick={() => setMachineAction(null)}>{nextWord("cloudActionCancel")}</button>
      </div>
    </dialog>
  </>
}

function emptyMachine(reading: Reading | undefined): boolean {
  return reading?.phase === "settled" && reading.value.kind === "ready" &&
    reading.value.complete && reading.value.rows.length === 0
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
