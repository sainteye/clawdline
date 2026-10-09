import { useEffect, useRef, useState, type ReactNode } from "react"
import { nextWord } from "../next-strings.js"
import { TranscriptEntries } from "../session/Transcript.js"
import { SessionToolbar } from "../session/SessionToolbar.js"
import { CloudAllTerminalList } from "../session/CloudAllTerminalList.js"
import { sessionsTerminalMode } from "../page-route.js"
import "../session/terminal-list.css"
import { AttentionOverview } from "./AttentionOverview.js"
import { fromListProjection } from "./attention-adapter.js"
import {
  afterEventGap, checkedProjection, destinationAvailable, destinationFragment, destinationFromFragment, destinationKey,
  prependOlderPage, projectionRefreshAt, SessionDetailCache, type MachineSessionProjection, type ProjectionProblem,
  type SessionContent, type SessionDestination, type SessionProjectionSource,
} from "./all-machine-sessions.js"
import { STATUS_FRESH_MS } from "./status-projection.js"

export interface FleetMachine {
  id: string
  name: string
  platform: string
  freshness: "current" | "stale" | "unknown"
}

/** A pinned, current destination for the independent detail actions feature. */
export interface DetailActionContext {
  destination: SessionDestination
  machine: FleetMachine
  row: Extract<MachineSessionProjection, { kind: "ready" }>["rows"][number]
  projection: Extract<MachineSessionProjection, { kind: "ready" }>
  content: SessionContent | null
}

type Reading = { phase: "loading" } | { phase: "settled"; value: MachineSessionProjection }

/** Only the hosted Cloud gate mounts this view; its source is the ss/ adapter. */
export type MachineToolbarAction = "terminal" | "voice" | "work" | "start" | "start_terminal"

export function AllMachineSessions({ machines, source, detailActions, detailExtras, fleetControls, onMachineAction }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  detailActions?: (context: DetailActionContext) => ReactNode
  detailExtras?: (context: DetailActionContext) => ReactNode
  fleetControls?: ReactNode
  onMachineAction?: (machineID: string, action: MachineToolbarAction) => void
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const [query, setQuery] = useState("")
  const [terminalMode, setTerminalMode] = useState(() => sessionsTerminalMode(location.hash))
  const [machineAction, setMachineAction] = useState<MachineToolbarAction | null>(null)
  const [compact, setCompact] = useState(() => window.matchMedia("(max-width: 899px)").matches)
  const [destination, setDestination] = useState<SessionDestination | null>(() => destinationFromFragment(location.hash))
  const [detail, setDetail] = useState<{ key: string; value: SessionContent | null; loading: boolean } | null>(null)
  const [older, setOlder] = useState<{ key: string; loading: boolean; error: Extract<SessionContent,
    { kind: "unavailable" }>["reason"] | null } | null>(null)
  const [questionRevision, setQuestionRevision] = useState(0)
  const cache = useRef(new SessionDetailCache())
  const destinationRef = useRef(destination)
  const detailRef = useRef(detail)
  const olderAbort = useRef<AbortController | null>(null)
  const refreshAbort = useRef<AbortController | null>(null)
  const heading = useRef<HTMLHeadingElement | null>(null)
  const listHeading = useRef<HTMLHeadingElement | null>(null)
  const searchField = useRef<HTMLInputElement | null>(null)
  const machineDialog = useRef<HTMLDialogElement | null>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  destinationRef.current = destination
  detailRef.current = detail

  useEffect(() => {
    const media = window.matchMedia("(max-width: 899px)")
    const change = () => setCompact(media.matches)
    media.addEventListener("change", change)
    return () => media.removeEventListener("change", change)
  }, [])

  useEffect(() => {
    const route = () => {
      setTerminalMode(sessionsTerminalMode(location.hash))
      setDestination(destinationFromFragment(location.hash))
    }
    window.addEventListener("hashchange", route)
    window.addEventListener("popstate", route)
    return () => {
      window.removeEventListener("hashchange", route)
      window.removeEventListener("popstate", route)
    }
  }, [])

  useEffect(() => {
    if (machineAction) machineDialog.current?.showModal()
    else machineDialog.current?.close()
  }, [machineAction])

  const chooseAction = (action: MachineToolbarAction) => {
    if (!onMachineAction || machines.length === 0) return
    if (machines.length === 1) onMachineAction(machines[0].id, action)
    else setMachineAction(action)
  }

  useEffect(() => {
    if (!destination) return
    const frame = window.requestAnimationFrame(() => heading.current?.focus())
    return () => window.cancelAnimationFrame(frame)
  }, [destination?.machineID, destination?.sessionID, destination?.executionGeneration])

  useEffect(() => {
    if (!source) {
      setReadings(Object.fromEntries(machines.map((machine) => [machine.id, {
        phase: "settled", value: { kind: "unavailable", reason: "old_version" },
      }])))
      return
    }
    const aborts = new Map<string, AbortController>()
    let live = true
    const read = (id: string) => {
      aborts.get(id)?.abort()
      const abort = new AbortController()
      aborts.set(id, abort)
      setReadings((before) => ({ ...before, [id]: before[id]?.phase === "settled" &&
        before[id].value.kind === "unavailable" && before[id].value.reason === "event_gap"
        ? before[id] : { phase: "loading" } }))
      void source.readMachine(id, abort.signal).then((value) => {
        if (live && !abort.signal.aborted) setReadings((before) => ({
          ...before, [id]: { phase: "settled", value: checkedProjection(id, value) },
        }))
      }, (error: unknown) => {
        if (live && !abort.signal.aborted) setReadings((before) => ({
          ...before, [id]: { phase: "settled", value: { kind: "unavailable", reason: problemOf(error) } },
        }))
      })
    }
    const stop = source.subscribe(({ machineID, sessionID, kind }) => {
      if (kind === "detail_changed") {
        const selected = destinationRef.current
        if (!selected || selected.machineID !== machineID || selected.sessionID !== sessionID) return
        const key = destinationKey(selected)
        cache.current.invalidateMachine(machineID)
        setDetail((before) => before?.key === key && before.value?.kind === "ready"
          ? { ...before, value: { ...before.value, question: null } } : before)
        setQuestionRevision((before) => before + 1)
        if (detailRef.current?.key !== key || detailRef.current.value?.kind !== "ready") return
        refreshAbort.current?.abort()
        olderAbort.current?.abort()
        olderAbort.current = null
        setOlder(null)
        const abort = new AbortController()
        refreshAbort.current = abort
        void source.readDetail(selected, abort.signal).then((value) => {
          if (abort.signal.aborted || !destinationRef.current || destinationKey(destinationRef.current) !== key) return
          if (value.kind === "ready" && (destinationKey(value.destination) !== key || !cache.current.put(value))) {
            setDetail({ key, value: { kind: "unavailable", reason: "unknown" }, loading: false })
            return
          }
          setDetail({ key, value, loading: false })
        }, (error: unknown) => {
          if (!abort.signal.aborted) setDetail({ key, value: { kind: "unavailable", reason: contentProblemOf(error) }, loading: false })
        }).finally(() => { if (refreshAbort.current === abort) refreshAbort.current = null })
        return
      }
      if (!machines.some((machine) => machine.id === machineID)) return
      cache.current.invalidateMachine(machineID)
      if (kind === "gap") setReadings((before) => ({ ...before, [machineID]: {
        phase: "settled", value: afterEventGap(before[machineID]?.phase === "settled" ? before[machineID].value : undefined),
      } }))
      read(machineID)
    })
    for (const machine of machines) {
      cache.current.invalidateMachine(machine.id)
      read(machine.id)
    }
    return () => {
      live = false
      stop()
      refreshAbort.current?.abort()
      for (const abort of aborts.values()) abort.abort()
    }
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
          if (!abort.signal.aborted) setReadings((before) => ({ ...before,
            [machineID]: { phase: "settled", value: checkedProjection(machineID, value) },
          }))
        }, (error: unknown) => {
          if (!abort.signal.aborted) setReadings((before) => ({ ...before,
            [machineID]: { phase: "settled", value: { kind: "unavailable", reason: problemOf(error) } },
          }))
        })
      }
    }, Math.max(1, earliest - Date.now()))
    return () => { window.clearTimeout(timer); abort.abort() }
  }, [readings, source, machines.map((machine) => machine.id).join("\0")])

  const detailReading = destination ? readings[destination.machineID] : undefined
  const named = destination ? machines.find((machine) => machine.id === destination.machineID) : null
  const observedProjection = detailReading?.phase === "settled" ? detailReading.value : undefined
  const projection: MachineSessionProjection | undefined = named && named.freshness !== "current"
    ? { kind: "unavailable", reason: named.freshness === "stale" ? "stale" : "unknown",
      observedAt: observedProjection?.observedAt, snapshotGeneration: observedProjection?.snapshotGeneration,
      rows: observedProjection?.rows?.map((item) => ({ ...item, freshness: "stale" })) }
    : observedProjection
  const availability = destination ? destinationAvailable(destination, projection) : "waiting"
  useEffect(() => {
    refreshAbort.current?.abort()
    refreshAbort.current = null
    olderAbort.current?.abort()
    olderAbort.current = null
    setOlder(null)
    if (!destination || !source || availability !== "ready") {
      setDetail(null)
      return
    }
    const key = destinationKey(destination)
    const abort = new AbortController()
    setDetail({ key, value: null, loading: true })
    void source.readDetail(destination, abort.signal).then((value) => {
      if (abort.signal.aborted) return
      if (value.kind === "ready") {
        if (destinationKey(value.destination) !== key || !cache.current.put(value)) {
          setDetail({ key, value: { kind: "unavailable", reason: "unknown" }, loading: false })
          return
        }
      }
      setDetail({ key, value, loading: false })
    }, (error: unknown) => {
      if (!abort.signal.aborted) setDetail({ key, value: { kind: "unavailable", reason: contentProblemOf(error) }, loading: false })
    })
    return () => {
      abort.abort()
      refreshAbort.current?.abort()
      olderAbort.current?.abort()
      source.closeDetail?.(destination)
    }
  }, [source, destination?.machineID, destination?.sessionID, destination?.executionGeneration, availability])

  useEffect(() => {
    if (!destination || !source?.readQuestion || detail?.key !== destinationKey(destination) ||
      detail.loading || detail.value?.kind !== "ready" || availability !== "ready") return
    const abort = new AbortController()
    const key = destinationKey(destination)
    void source.readQuestion(destination, abort.signal).then((question) => {
      if (abort.signal.aborted || !destinationRef.current || destinationKey(destinationRef.current) !== key) return
      setDetail((before) => before?.key === key && before.value?.kind === "ready"
        ? { ...before, value: { ...before.value, question } } : before)
    }, () => undefined)
    return () => abort.abort()
  }, [source, destination?.machineID, destination?.sessionID, destination?.executionGeneration,
    detail?.key, detail?.loading, detail?.value?.kind, availability, questionRevision])

  const search = query.trim().toLocaleLowerCase()
  const shown = machines.map((machine) => {
    const reading = readings[machine.id]
    const rows = reading?.phase === "settled" && reading.value.rows
      ? reading.value.rows.filter((row) => !search ||
        `${machine.name} ${machine.id} ${machine.platform} ${row.title} ${row.destination.sessionID} ${stateWord(row.state)}`
          .toLocaleLowerCase().includes(search)) : []
    return { machine, reading, rows }
  }).filter(({ machine, rows }) => !search || rows.length > 0 ||
    `${machine.name} ${machine.id} ${machine.platform}`.toLocaleLowerCase().includes(search))
  const row = projection?.rows && destination
    ? projection.rows.find((candidate) => destinationKey(candidate.destination) === destinationKey(destination)) : null
  const actionContext: DetailActionContext | null = availability === "ready" && destination && named &&
    projection?.kind === "ready" && row ? {
      destination, machine: named, row, projection,
      content: detail?.key === destinationKey(destination) ? detail.value : null,
    } : null

  const closeDetail = () => {
    olderAbort.current?.abort()
    history.replaceState(history.state, "", location.pathname + location.search)
    setDestination(null)
    window.requestAnimationFrame(() => {
      if (returnFocus.current?.isConnected) returnFocus.current.focus()
      else listHeading.current?.focus()
      returnFocus.current = null
    })
  }
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const modal = [...document.querySelectorAll('dialog[open], [role="dialog"][aria-modal="true"]')]
        .some((element) => element.getClientRects().length > 0)
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || modal) return
      const active = document.activeElement
      const typing = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement ||
        (active instanceof HTMLElement && active.isContentEditable)
      if (event.key === "Escape") {
        if (destinationRef.current) { event.preventDefault(); closeDetail(); return }
        if (active === searchField.current) {
          event.preventDefault()
          if (query) setQuery("")
          else searchField.current?.blur()
        }
        return
      }
      if (typing) return
      if (event.key === "/") {
        event.preventDefault()
        searchField.current?.focus()
        searchField.current?.select()
        return
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [query])
  const loadOlder = () => {
    const current = detailRef.current
    if (!destination || !source?.readOlder || availability !== "ready" || current?.value?.kind !== "ready" ||
      current.key !== destinationKey(destination) || !Number.isSafeInteger(current.value.nextBefore) ||
      !current.value.nextBefore || olderAbort.current) return
    const key = current.key
    const before = current.value.nextBefore
    const abort = new AbortController()
    olderAbort.current = abort
    setOlder({ key, loading: true, error: null })
    void source.readOlder(destination, before, abort.signal).then((page) => {
      if (abort.signal.aborted || !destinationRef.current || destinationKey(destinationRef.current) !== key) return
      if (page.kind === "unavailable") {
        setOlder({ key, loading: false, error: page.reason })
        return
      }
      const shown = detailRef.current
      const merged = shown?.value ? prependOlderPage(shown.value, page) : null
      if (!merged || shown?.key !== key) {
        setOlder({ key, loading: false, error: "unknown" })
        return
      }
      cache.current.put(merged)
      setDetail({ key, value: merged, loading: false })
      setOlder(null)
    }, (error: unknown) => {
      if (!abort.signal.aborted) setOlder({ key, loading: false, error: contentProblemOf(error) })
    }).finally(() => { if (olderAbort.current === abort) olderAbort.current = null })
  }
  const listPane = <section className="pane pane-list cloud-all-list" inert={compact && !!destination}
    aria-hidden={compact && !!destination ? true : undefined}>
    <SessionToolbar inputRef={searchField} filter={query} onFilter={setQuery} terminalMode={terminalMode}
      placeholder={nextWord("cloudAllSearch")} label={nextWord("cloudAllSearch")}
      onSessions={() => { location.hash = machines.length > 1 ? "#all-machines" : "#page=sessions"; setTerminalMode(false) }}
      onTerminals={() => chooseAction("terminal")}
      onVoice={() => chooseAction("voice")} onWork={() => chooseAction("work")}
      onStart={() => chooseAction(terminalMode ? "start_terminal" : "start")} />
    <div className="scroller list-scroll cloud-all-list-scroll">
    {terminalMode ? <CloudAllTerminalList shown={true} filter={query} /> : <>
    <div className="cloud-all-groups">
      {shown.map(({ machine, reading, rows }) => <section key={machine.id} className="cloud-all-group" aria-label={`${machine.name} ${machine.id}`}>
        <h2>{machine.name} <small>{machine.platform}</small></h2>
        <p className="cloud-all-id">{nextWord("cloudAllMachineID")}: {machine.id}</p>
        {machine.freshness !== "current" && reading?.phase === "settled" && reading.value.kind === "ready" && <p role="status">{machine.freshness === "stale"
          ? nextWord("cloudAllStaleSource") : nextWord("cloudAllUnknownSource")}</p>}
        {!reading || reading.phase === "loading" ? <p role="status">{nextWord("cloudAllLoading")}</p>
          : <>
            {reading.value.kind === "unavailable" && <p role="status">{problemWord(reading.value.reason)}</p>}
            {rows.length === 0 ? reading.value.unknownTargets ? null : reading.value.kind === "ready" ? <p>{reading.value.rows.length === 0
            ? nextWord("cloudAllEmptyMachine") : nextWord("cloudAllNoMatch")}</p>
            : null : <ul>{rows.map((row) => <li key={destinationKey(row.destination)}>
            <a href={destinationFragment(row.destination)}
              aria-current={destination && destinationKey(destination) === destinationKey(row.destination) ? "true" : undefined}
              onClick={(event) => {
              returnFocus.current = event.currentTarget
              setDestination(row.destination)
            }}>
              <strong>{row.title || row.destination.sessionID}</strong>
              <span className="cloud-all-id">{nextWord("cloudAllSessionID")}: {row.destination.sessionID}</span>
              <span>{stateWord(row.state)} · {timeWord(row.observedAt)} · {nextWord(row.freshness === "current" ? "cloudAllCurrent" :
                row.freshness === "stale" ? "cloudAllStale" : "cloudAllUnknown")}{row.needsAttention ? " · " + nextWord("cloudAllAttentionNeeded") : ""}</span>
            </a>
          </li>)}</ul>}
          </>}
        {reading?.phase === "settled" && !!reading.value.unknownTargets &&
          <p role="status">{nextWord("cloudAllUnknownTargets", { count: reading.value.unknownTargets })}</p>}
      </section>)}
    </div>
    {shown.length === 0 && <p>{nextWord("cloudAllNoMatch")}</p>}
    </>}
    </div>
  </section>
  return <main className="app cloud-all" data-page-view="sessions" data-view={destination ? "detail" : "list"}
    data-pane="on" data-mode={terminalMode ? "terminal" : undefined} data-step="all-sessions">
    <header className="cloud-all-header">
      <h1 ref={listHeading} tabIndex={-1}>{machines.length === 1 ? nextWord("cloudSingleSessions") : nextWord("cloudAllMachines")}</h1>
    </header>
    {listPane}
    {destination && <section className="pane pane-detail cloud-all-main" aria-live="polite">
      <header className="cloud-all-detail-head">
        <button type="button" className="cloud-all-detail-back" onClick={closeDetail}>{nextWord("cloudAllBack")}</button>
        <h2 ref={heading} tabIndex={-1}>{detail?.key === destinationKey(destination) && detail.value?.kind === "ready"
          ? detail.value.info.title || row?.title || destination.sessionID : row?.title || destination.sessionID}</h2>
        <span>{named?.name ?? destination.machineID}</span>
      </header>
      <div className="cloud-all-detail-scroll">
      <dl className="cloud-all-target">
        <div><dt>{nextWord("cloudAllSessionID")}</dt><dd className="cloud-all-id">{destination.sessionID}</dd></div>
        <div><dt>{nextWord("cloudAllDataTime")}</dt><dd>{detail?.key === destinationKey(destination) && detail.value?.kind === "ready"
          ? timeWord(detail.value.observedAt) : row ? timeWord(row.observedAt) : nextWord("cloudAllUnknown")}</dd></div>
      </dl>
      <details className="cloud-all-identity"><summary>{nextWord("cloudAllGeneration")}</summary>
        <p>{nextWord("cloudAllMachineID")}: <code>{destination.machineID}</code></p>
        <p>{nextWord("cloudAllGeneration")}: <code>{destination.executionGeneration}</code></p>
      </details>
      {row && <details className="cloud-all-work"
        aria-label={nextWord("cloudAllWorkStatus")}>
        <summary>{nextWord("cloudAllWorkStatus")} · {stateWord(row.state)} · {nextWord(row.freshness === "current" && availability === "ready" ? "cloudAllCurrent" : "cloudAllStale")}</summary>
        <dl>
          <div><dt>{nextWord("cloudAllState")}</dt><dd>{stateWord(row.state)}</dd></div>
          <div><dt>{nextWord("cloudAllWaitingForReply")}</dt><dd>{factWord(row.waitingForReply)}</dd></div>
          <div><dt>{nextWord("cloudAllNoMovement")}</dt><dd>{factWord(row.noMovement)}</dd></div>
          <div><dt>{nextWord("cloudAllCloseBlocked")}</dt><dd>{factWord(row.closeBlocked)}</dd></div>
          <div><dt>{nextWord("cloudAllFailedAgents")}</dt><dd>{row.failedAgentCount ?? nextWord("cloudAllUnknown")}</dd></div>
          <div><dt>{nextWord("cloudAllCompletedUnconfirmed")}</dt><dd>{factWord(row.completedUnconfirmed)}</dd></div>
          <div><dt>{nextWord("cloudAllLastMovement")}</dt><dd>{row.lastMovementAt === undefined
            ? nextWord("cloudAllUnknown") : timeWord(row.lastMovementAt)}</dd></div>
          <div><dt>{nextWord("cloudAllStatusTime")}</dt><dd>{timeWord(projection?.observedAt ?? row.observedAt)}</dd></div>
        </dl>
      </details>}
      {!named ? <p role="alert">{nextWord("cloudAllUnknownSource")}</p>
        : availability === "changed" ? <p role="alert">{nextWord("cloudAllChanged")}</p>
        : availability === "stale" ? <p role="status">{nextWord("cloudAllStaleSource")}</p>
        : availability === "unknown" ? <p role="status">{nextWord("cloudAllUnknownSource")}</p>
        : availability === "waiting" ? <p role="status">{projection?.kind === "unavailable"
          ? problemWord(projection.reason) : nextWord("cloudAllLoading")}</p>
        : !detail || detail.loading || detail.key !== destinationKey(destination) ? <p role="status">{nextWord("cloudAllLoadingDetail")}</p>
        : detail.value?.kind === "unavailable" ? <p role="alert">{contentWord(detail.value.reason)}</p>
        : <section aria-label={nextWord("cloudAllConversation")} className="cloud-all-content">
          {(detail.value!.info.assistant || detail.value!.info.model) && <p className="cloud-all-provider">{[detail.value!.info.assistant,
            detail.value!.info.model].filter(Boolean).join(" · ")}</p>}
          {detail.value!.nextBefore !== undefined && source?.readOlder && <div className="cloud-all-older">
            <button type="button" onClick={loadOlder} disabled={older?.key === detail.key && older.loading}
              aria-busy={older?.key === detail.key && older.loading ? "true" : undefined}>
              {older?.key === detail.key && older.loading ? nextWord("cloudAllLoadingOlder") : nextWord("cloudAllLoadOlder")}
            </button>
            {older?.key === detail.key && older.error && <p role="alert">{older.error === "old_version"
              ? nextWord("cloudAllOlderOldVersion") : contentWord(older.error)}</p>}
          </div>}
          {detail.value!.entries.length === 0 ? <p>{nextWord("cloudAllNoContent")}</p> :
            <div className="tx cloud-all-transcript"><TranscriptEntries key={destinationKey(destination)}
              entries={detail.value!.entries} assistant={detail.value!.info.assistant}
              working={row?.state === "working"} /></div>}
        </section>}
      {actionContext && detailExtras?.(actionContext)}
      </div>
      {actionContext && detailActions && <div className="cloud-all-action-bar">{detailActions(actionContext)}</div>}
    </section>}
    {!destination && !terminalMode && <section className="pane pane-detail cloud-all-overview" inert={compact}
      aria-hidden={compact ? true : undefined}>
      <div className="cloud-all-overview-scroll">
        <h2>{machines.length === 1 ? nextWord("cloudSingleSessions") : nextWord("cloudAllMachines")}</h2>
        <p className="cloud-all-lede">{nextWord("cloudAllLede")}</p>
        <AttentionOverview
          reading={{ phase: "ready", machines: machines.map((machine) => fromListProjection(machine, readings[machine.id])), observedNow: Date.now() }}
          locale={document.documentElement.lang === "zh-Hant" ? "zh-Hant-TW" : "en"}
          onOpen={(target) => {
            const selected = { machineID: target.machine_id, sessionID: target.session_id,
              executionGeneration: target.execution_generation }
            returnFocus.current = null
            location.hash = destinationFragment(selected)
            setDestination(selected)
          }}
        />
        {fleetControls}
      </div>
    </section>}
    {terminalMode && <section className="pane pane-detail cloud-all-overview" inert={compact}
      aria-hidden={compact ? true : undefined}><div className="cloud-all-overview-scroll">
        <h2>{nextWord("terminalListMode")}</h2>
      </div></section>}
    <dialog ref={machineDialog} className="cloud-all-machine-dialog" aria-label={nextWord("cloudSwitch")}
      onCancel={(event) => { event.preventDefault(); setMachineAction(null) }}>
      <h2>{nextWord("cloudSwitch")}</h2>
      {machines.map((machine) => <button key={machine.id} type="button" onClick={() => {
        const action = machineAction
        setMachineAction(null)
        if (action) onMachineAction?.(machine.id, action)
      }}>{machine.name} · {machine.platform}</button>)}
      <button type="button" onClick={() => setMachineAction(null)}>{nextWord("cloudActionCancel")}</button>
    </dialog>
  </main>
}

function problemOf(error: unknown): ProjectionProblem {
  const code = (error as { code?: unknown } | null)?.code
  return code === "offline" || code === "stale" || code === "old_version" || code === "no_permission" || code === "event_gap"
    ? code : "unresponsive"
}

function contentProblemOf(error: unknown): Extract<SessionContent, { kind: "unavailable" }>["reason"] {
  const code = (error as { code?: unknown } | null)?.code
  return code === "no_permission" || code === "old_version" || code === "offline" || code === "stale" || code === "changed" ? code : "unknown"
}

function problemWord(reason: ProjectionProblem): string {
  return nextWord(({ offline: "cloudAllOffline", stale: "cloudAllStaleSource", unknown: "cloudAllUnknownSource",
    old_version: "cloudAllOldVersion", no_permission: "cloudAllNoPermission", unresponsive: "cloudAllUnresponsive",
    event_gap: "cloudAllEventGap", bad_projection: "cloudAllBadProjection" })[reason] as "cloudAllOffline")
}

function contentWord(reason: Extract<SessionContent, { kind: "unavailable" }>["reason"]): string {
  return nextWord(({ no_permission: "cloudAllNoContentPermission", old_version: "cloudAllOldVersion",
    unknown: "cloudAllUnknownSource", offline: "cloudAllOffline", stale: "cloudAllStaleSource",
    changed: "cloudAllChanged" })[reason] as "cloudAllOffline")
}

function timeWord(at: number): string {
  return Number.isFinite(at) ? new Date(at).toLocaleString() : nextWord("cloudAllUnknown")
}

function factWord(value: boolean | undefined): string {
  return value === undefined ? nextWord("cloudAllUnknown") : nextWord(value ? "cloudAllYes" : "cloudAllNo")
}

function stateWord(state: string): string {
  switch (state) {
    case "working": return nextWord("cloudAllStateWorking")
    case "waiting": return nextWord("cloudAllStateWaiting")
    case "idle": return nextWord("cloudAllStateIdle")
    default: return nextWord("cloudAllUnknown")
  }
}
