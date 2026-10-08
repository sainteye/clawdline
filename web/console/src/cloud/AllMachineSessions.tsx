import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { nextWord } from "../next-strings.js"
import { AttentionOverview } from "./AttentionOverview.js"
import { fromListProjection } from "./attention-adapter.js"
import {
  afterEventGap, checkedProjection, destinationAvailable, destinationFragment, destinationFromFragment, destinationKey,
  matchesSession, SessionDetailCache, type MachineSessionProjection, type ProjectionProblem,
  type SessionContent, type SessionDestination, type SessionFilters, type SessionProjectionSource,
} from "./all-machine-sessions.js"

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
const emptyFilters: SessionFilters = { machine: "", platform: "", state: "", freshness: "all", attention: "all" }

/** Only the hosted Cloud gate mounts this view; its source is the ss/ adapter. */
export function AllMachineSessions({ machines, source, onClose, embedded = false, detailActions, fleetControls }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  onClose?: () => void
  embedded?: boolean
  detailActions?: (context: DetailActionContext) => ReactNode
  fleetControls?: ReactNode
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const [filters, setFilters] = useState(emptyFilters)
  const [destination, setDestination] = useState<SessionDestination | null>(() => destinationFromFragment(location.hash))
  const [detail, setDetail] = useState<{ key: string; value: SessionContent | null; loading: boolean } | null>(null)
  const [questionRevision, setQuestionRevision] = useState(0)
  const cache = useRef(new SessionDetailCache())
  const destinationRef = useRef(destination)
  destinationRef.current = destination

  useEffect(() => {
    const route = () => setDestination(destinationFromFragment(location.hash))
    window.addEventListener("hashchange", route)
    window.addEventListener("popstate", route)
    return () => {
      window.removeEventListener("hashchange", route)
      window.removeEventListener("popstate", route)
    }
  }, [])

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
      for (const abort of aborts.values()) abort.abort()
    }
  }, [source, machines.map((machine) => machine.id + ":" + machine.freshness).join("\0")])

  const detailReading = destination ? readings[destination.machineID] : undefined
  const named = destination ? machines.find((machine) => machine.id === destination.machineID) : null
  const projection: MachineSessionProjection | undefined = named && named.freshness !== "current"
    ? { kind: "unavailable", reason: named.freshness === "stale" ? "stale" : "unknown" }
    : detailReading?.phase === "settled" ? detailReading.value : undefined
  const availability = destination ? destinationAvailable(destination, projection) : "waiting"
  useEffect(() => {
    if (!destination || !source || availability !== "ready") {
      setDetail(null)
      return
    }
    const key = destinationKey(destination)
    const held = cache.current.get(destination)
    if (held) {
      setDetail({ key, value: held, loading: false })
      return
    }
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

  const states = useMemo(() => [...new Set(Object.values(readings).flatMap((reading) =>
    reading.phase === "settled" && reading.value.kind === "ready" ? reading.value.rows.map((row) => row.state) : [],
  ))].sort(), [readings])
  const platforms = [...new Set(machines.map((machine) => machine.platform))]
  const shown = machines.filter((machine) => !filters.machine || filters.machine === machine.id).map((machine) => {
    const reading = readings[machine.id]
    const rows = reading?.phase === "settled" && reading.value.kind === "ready"
      ? reading.value.rows.filter((row) => matchesSession(row, machine.platform, filters)) : []
    return { machine, reading, rows }
  })
  const row = projection?.kind === "ready" && destination
    ? projection.rows.find((candidate) => destinationKey(candidate.destination) === destinationKey(destination)) : null

  const closeDetail = () => {
    history.replaceState(history.state, "", location.pathname + location.search)
    setDestination(null)
  }
  return <div className="cloud-all" data-embedded={embedded ? "true" : undefined} data-step="all-sessions">
    <header className="cloud-all-header">
      {(destination || onClose) && <button type="button" onClick={destination ? closeDetail : onClose}>{nextWord("cloudAllBack")}</button>}
      <h1>{destination ? nextWord("cloudAllDetail") : machines.length === 1 ? nextWord("cloudSingleSessions") : nextWord("cloudAllMachines")}</h1>
    </header>
    {destination ? <main className="cloud-all-main" aria-live="polite">
      <dl className="cloud-all-target">
        <div><dt>{nextWord("cloudAllMachine")}</dt><dd>{named?.name ?? destination.machineID}</dd></div>
        <div><dt>{nextWord("cloudAllPlatform")}</dt><dd>{named?.platform ?? nextWord("cloudAllUnknown")}</dd></div>
        <div><dt>{nextWord("cloudAllGeneration")}</dt><dd>{destination.executionGeneration}</dd></div>
        <div><dt>{nextWord("cloudAllDataTime")}</dt><dd>{row ? timeWord(row.observedAt) : nextWord("cloudAllUnknown")}</dd></div>
      </dl>
      {availability === "ready" && named && projection?.kind === "ready" && row && detailActions?.({
        destination, machine: named, row, projection,
        content: detail?.key === destinationKey(destination) ? detail.value : null,
      })}
      {!named ? <p role="alert">{nextWord("cloudAllUnknownSource")}</p>
        : availability === "changed" ? <p role="alert">{nextWord("cloudAllChanged")}</p>
        : availability === "stale" ? <p role="status">{nextWord("cloudAllStaleSource")}</p>
        : availability === "unknown" ? <p role="status">{nextWord("cloudAllUnknownSource")}</p>
        : availability === "waiting" ? <p role="status">{projection?.kind === "unavailable"
          ? problemWord(projection.reason) : nextWord("cloudAllLoading")}</p>
        : !detail || detail.loading || detail.key !== destinationKey(destination) ? <p role="status">{nextWord("cloudAllLoadingDetail")}</p>
        : detail.value?.kind === "unavailable" ? <p role="alert">{contentWord(detail.value.reason)}</p>
        : <section aria-label={nextWord("cloudAllConversation")} className="cloud-all-content">
          <p className="cloud-all-time">{nextWord("cloudAllDataAt", { time: timeWord(detail.value!.observedAt) })}</p>
          {detail.value!.info.title && <h2>{detail.value!.info.title}</h2>}
          {(detail.value!.info.assistant || detail.value!.info.model) && <p>{[detail.value!.info.assistant,
            detail.value!.info.model].filter(Boolean).join(" · ")}</p>}
          {detail.value!.entries.length === 0 ? <p>{nextWord("cloudAllNoContent")}</p> : detail.value!.entries.map((entry, index) =>
            <article key={index}><h2>{entry.speaker}</h2><p>{entry.text}</p></article>)}
        </section>}
    </main> : <main className="cloud-all-main">
      <p className="cloud-all-lede">{nextWord("cloudAllLede")}</p>
      {fleetControls}
      {!embedded && <AttentionOverview
        reading={{ phase: "ready", machines: machines.map((machine) => fromListProjection(machine, readings[machine.id])), observedNow: Date.now() }}
        locale={document.documentElement.lang === "zh-Hant" ? "zh-Hant-TW" : "en"}
        onOpen={(target) => {
          const selected = { machineID: target.machine_id, sessionID: target.session_id,
            executionGeneration: target.execution_generation }
          location.hash = destinationFragment(selected)
          setDestination(selected)
        }}
      />}
      <div className="cloud-all-filters">
        <label>{nextWord("cloudAllMachine")}<select value={filters.machine} onChange={(event) => setFilters({ ...filters, machine: event.target.value })}>
          <option value="">{nextWord("cloudAllAny")}</option>{machines.map((machine) => <option value={machine.id} key={machine.id}>{machine.name}</option>)}
        </select></label>
        <label>{nextWord("cloudAllPlatform")}<select value={filters.platform} onChange={(event) => setFilters({ ...filters, platform: event.target.value })}>
          <option value="">{nextWord("cloudAllAny")}</option>{platforms.map((platform) => <option value={platform} key={platform}>{platform}</option>)}
        </select></label>
        <label>{nextWord("cloudAllState")}<select value={filters.state} onChange={(event) => setFilters({ ...filters, state: event.target.value })}>
          <option value="">{nextWord("cloudAllAny")}</option>{states.map((state) => <option value={state} key={state}>{stateWord(state)}</option>)}
        </select></label>
        <label>{nextWord("cloudAllFreshness")}<select value={filters.freshness} onChange={(event) => setFilters({ ...filters, freshness: event.target.value as SessionFilters["freshness"] })}>
          <option value="all">{nextWord("cloudAllAny")}</option><option value="current">{nextWord("cloudAllCurrent")}</option><option value="stale">{nextWord("cloudAllStale")}</option><option value="unknown">{nextWord("cloudAllUnknown")}</option>
        </select></label>
        <label>{nextWord("cloudAllAttention")}<select value={filters.attention} onChange={(event) => setFilters({ ...filters, attention: event.target.value as SessionFilters["attention"] })}>
          <option value="all">{nextWord("cloudAllAny")}</option><option value="needed">{nextWord("cloudAllAttentionNeeded")}</option>
        </select></label>
      </div>
      <div className="cloud-all-groups">
        {shown.map(({ machine, reading, rows }) => <section key={machine.id} className="cloud-all-group" aria-label={machine.name}>
          <h2>{machine.name} <small>{machine.platform}</small></h2>
          {machine.freshness !== "current" && reading?.phase === "settled" && reading.value.kind === "ready" && <p role="status">{machine.freshness === "stale"
            ? nextWord("cloudAllStaleSource") : nextWord("cloudAllUnknownSource")}</p>}
          {!reading || reading.phase === "loading" ? <p role="status">{nextWord("cloudAllLoading")}</p>
            : reading.value.kind === "unavailable" ? <p role="status">{problemWord(reading.value.reason)}</p>
            : rows.length === 0 ? reading.value.unknownTargets ? null : <p>{reading.value.rows.length === 0
              ? nextWord("cloudAllEmptyMachine") : nextWord("cloudAllNoMatch")}</p>
            : <ul>{rows.map((row) => <li key={destinationKey(row.destination)}>
              <a href={destinationFragment(row.destination)} onClick={() => setDestination(row.destination)}>
                <strong>{row.title || row.destination.sessionID}</strong>
                <span>{stateWord(row.state)} · {timeWord(row.observedAt)}{row.needsAttention ? " · " + nextWord("cloudAllAttentionNeeded") : ""}</span>
              </a>
            </li>)}</ul>}
          {reading?.phase === "settled" && reading.value.kind === "ready" && !!reading.value.unknownTargets &&
            <p role="status">{nextWord("cloudAllUnknownTargets", { count: reading.value.unknownTargets })}</p>}
        </section>)}
      </div>
      {shown.length === 0 && <p>{nextWord("cloudAllNoMatch")}</p>}
    </main>}
  </div>
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

function stateWord(state: string): string {
  switch (state) {
    case "working": return nextWord("cloudAllStateWorking")
    case "waiting": return nextWord("cloudAllStateWaiting")
    case "idle": return nextWord("cloudAllStateIdle")
    default: return nextWord("cloudAllUnknown")
  }
}
