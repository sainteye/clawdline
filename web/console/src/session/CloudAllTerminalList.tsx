import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react"
import { nextWord } from "../next-strings.js"
import { readProjectPlaces, type ProjectPlace } from "../pages/work/api.js"
import { terminalHost, watchTerminalHost, type TerminalHost } from "../cloud/terminal-host.js"
import { scheduleFleet, onScheduleFleet, type ScheduleFleet } from "../cloud/schedule-machines.js"
import { acquireTerminalConnection } from "../cloud/terminal-connection-owner.js"
import { TerminalObservation } from "../cloud/terminal-observation.js"
import { openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { holderWords, terminalRefusalWords, terminalShortID, terminalStatusWords } from "../pages/terminal/words.js"
import { collectCloudTerminals, readCloudTerminalList, type CloudTerminalRow, type CloudTerminalError } from "./cloud-terminal-all.js"
import { recentTerminalCloseStates, terminalCloseRevision, terminalCloseState, watchTerminalClose } from "../cloud/terminal-close-state.js"
import { openMachinePairing } from "../legacy/devices-bridge.js"
import { TerminalMarks } from "./TerminalGlyph.js"

async function readMachine(host: TerminalHost, machine: string): Promise<import("@clawdline/contract").Terminal[]> {
  const { session, release } = await acquireTerminalConnection(host, machine, TAB, new TerminalObservation())
  try {
    const answer = await readCloudTerminalList(() => session.request("list", { client: TAB }))
    const rows = answer.result?.terminals
    if (!Array.isArray(rows)) throw Object.assign(new Error("terminal_bad_receipt"), { code: "terminal_bad_receipt" })
    return rows as import("@clawdline/contract").Terminal[]
  } finally {
    release()
  }
}

/** The hosted tty list uses each paired machine's own encrypted terminal channel. */
export function CloudAllTerminalList({ shown, filter }: { shown: boolean; filter: string }) {
  const [host, setHost] = useState<TerminalHost | null>(terminalHost)
  const [fleet, setFleet] = useState<ScheduleFleet | null>(scheduleFleet)
  const places = useRef<ProjectPlace[]>([])
  const [rows, setRows] = useState<CloudTerminalRow[]>([])
  const [errors, setErrors] = useState<CloudTerminalError[]>([])
  const [loading, setLoading] = useState(true)
  const [projectError, setProjectError] = useState("")
  const [routeError, setRouteError] = useState("")
  const [machineFilter, setMachineFilter] = useState("")
  const [projectFilter, setProjectFilter] = useState("")
  const [refresh, setRefresh] = useState(0)
  useSyncExternalStore(watchTerminalClose, terminalCloseRevision, () => 0)
  const latestClose = recentTerminalCloseStates()[0]
  const generation = useRef(0)
  const currentRows = useRef(rows)
  currentRows.current = rows

  useEffect(() => watchTerminalHost(setHost), [])
  useEffect(() => onScheduleFleet(() => setFleet(scheduleFleet())), [])
  const fleetKey = JSON.stringify(fleet?.machines.map((machine) => [machine.id, machine.name]) ?? [])
  const reload = useCallback(async () => {
    if (!host || !fleet) return
    const mine = ++generation.current
    setLoading(true)
    setErrors([])
    setProjectError("")
    try {
      // Start machine reads while the Project directory is in flight. Its
      // names are needed to route a row, but not to ask a machine for rows.
      const nextPlaces = readProjectPlaces().then((page) => {
        if (mine === generation.current) places.current = page.places
        return page.places
      }, (error) => {
        if (mine === generation.current) setProjectError(error instanceof Error ? error.message : "project_list_failed")
        return places.current
      })
      const machines = fleet.machines.map((machine) => ({ id: machine.id, name: machine.name }))
      const next = await collectCloudTerminals(machines, nextPlaces, currentRows.current,
        (machine) => readMachine(host, machine),
        (progress) => { if (mine === generation.current) { setRows(progress.rows); setErrors(progress.errors) } })
      if (mine === generation.current) { setRows(next.rows); setErrors(next.errors) }
    } finally {
      if (mine === generation.current) setLoading(false)
    }
  }, [host, fleetKey, refresh])
  useEffect(() => {
    if (!shown) return
    void reload()
    return () => { ++generation.current }
  }, [shown, reload])
  const retry = () => setRefresh((value) => value + 1)
  const repairPairing = (machine: string) => {
    if (openMachinePairing(machine)) return
    const row = document.querySelector<HTMLButtonElement>('#sidebar [data-page-to="devices"]')
    if (row && !row.disabled) row.click()
  }

  const query = filter.trim().toLocaleLowerCase()
  const projectOptions = [...new Map(rows.filter((row) => row.project).map((row) => [row.project, row.projectName])).entries()]
  const matching = rows.filter((row) => {
    const close = terminalCloseState(row.machine, row.terminal.id)
    return close?.status !== "ok" && close?.status !== "ended" && (!machineFilter || row.machine === machineFilter) &&
      (!projectFilter || row.project === projectFilter) && (!query ||
      `${row.projectName} ${row.machineName} ${row.terminal.id} ${row.terminal.dir ?? ""}`.toLocaleLowerCase().includes(query))
  })
  const open = (row: CloudTerminalRow) => {
    if (!row.project) { setRouteError(nextWord("terminalAllUnknownProject")); return }
    openTerminalPage(row.project, row.terminal.id, "sessions")
  }

  if (!host) return <p className="terminal-list-message" role="status">{nextWord("terminalCloudLineReconnecting")}</p>
  return <div className="session-terminal-list" aria-busy={loading ? "true" : undefined}>
    <div className="session-terminal-head"><div><h2>{nextWord("terminalListMode")}</h2><p>{loading && !rows.length ? nextWord("terminalAllChecking") : nextWord("terminalListCount", { n: matching.length })}</p></div>
      <button type="button" onClick={retry} disabled={loading}>{nextWord("terminalCloudReloadList")}</button></div>
    <div className="session-terminal-filters">
      <label>{nextWord("terminalFilterMachine")}
        <select value={machineFilter} onChange={(event) => setMachineFilter(event.target.value)}>
          <option value="">{nextWord("terminalFilterAll")}</option>
          {fleet?.machines.map((machine) => <option key={machine.id} value={machine.id}>{machine.name}</option>)}
        </select>
      </label>
      <label>{nextWord("terminalFilterProject")}
        <select value={projectFilter} onChange={(event) => setProjectFilter(event.target.value)}>
          <option value="">{nextWord("terminalFilterAll")}</option>
          {projectOptions.map(([project, name]) => <option key={project} value={project}>{name}</option>)}
        </select>
      </label>
    </div>
    {projectError && <div className="terminal-list-message" role="alert">{nextWord("terminalProjectsFailedCode", { code: projectError })} <button type="button" onClick={retry}>{nextWord("terminalAllRetry")}</button></div>}
    {routeError && <p className="terminal-list-message" role="alert">{routeError}</p>}
    {latestClose && <p className="terminal-list-message" role="status">
      {nextWord("terminalIdentity", { id: terminalShortID(latestClose.terminal) })} · {nextWord(latestClose.status === "ok" ? "terminalTerminateSucceeded" :
        latestClose.status === "ended" ? "terminalTerminateEnded" : latestClose.status === "pending" ? "terminalTerminatePending" :
          latestClose.status === "unknown" ? "terminalTerminateUnknown" : "terminalCloudError",
        { code: terminalRefusalWords(latestClose.error) })}</p>}
    {errors.map((error) => {
      const machineName = fleet?.machines.find((machine) => machine.id === error.machine)?.name ?? error.machine
      const accessDenied = ["forbidden", "terminal_forbidden", "terminal_access_revoked"].includes(error.code)
      return <p className="terminal-list-message" role="alert" key={error.machine}>
        {accessDenied ? nextWord("terminalAllMachineAccessDenied", { machine: machineName }) :
          nextWord("terminalAllMachineFailed", { machine: machineName, why: error.code })} {accessDenied ?
          <button type="button" onClick={() => repairPairing(error.machine)}>{nextWord("terminalAllRepairPairing")}</button> :
          <button type="button" onClick={retry} disabled={loading}>{nextWord("terminalAllRetry")}</button>}</p>
    })}
    {loading && <p className="terminal-list-message terminal-list-progress" role="status"><span className="terminal-list-spinner" aria-hidden="true" />{nextWord("terminalListLoading")}</p>}
    {loading && !rows.length && <div className="terminal-list-skeleton" aria-hidden="true"><span /><span /></div>}
    {!loading && !projectError && !matching.length && (query || machineFilter || projectFilter) &&
      <p className="terminal-list-message">{nextWord("terminalAllEmptyFilter")}</p>}
    {!loading && !projectError && !errors.length && !matching.length && !query && !machineFilter && !projectFilter &&
      <p className="terminal-list-message">{nextWord("terminalAllCloudEmpty")}</p>}
    <ul className="session-terminal-rows">{matching.map((row) => <li key={`${row.machine}/${row.terminal.id}`}>
      <button className="session-terminal-card" type="button" onClick={() => open(row)}
        aria-label={nextWord("terminalOpenView", { project: row.projectName, machine: row.machineName, id: terminalShortID(row.terminal.id) })}>
        <TerminalMarks place={places.current.find((place) => place.id === row.project)} />
        <span className="session-terminal-main">
          <strong>{row.projectName}</strong>
          <span>{row.machineName} · {nextWord("terminalIdentity", { id: terminalShortID(row.terminal.id) })}</span>
          <span>{terminalStatusWords(row.terminal.status)}{row.terminal.control.held ? ` · ${holderWords(row.terminal.control.holder)}` : ""}</span>
          {terminalCloseState(row.machine, row.terminal.id)?.status === "unknown" && <span>{nextWord("terminalTerminateUnknown")}</span>}
          {terminalCloseState(row.machine, row.terminal.id)?.status === "pending" && <span>{nextWord("terminalTerminatePending")}</span>}
          {row.stale && <span>{nextWord("terminalAllLastConfirmed", { time: new Date(row.confirmedAt).toLocaleTimeString() })}</span>}
          <bdi className="session-terminal-dir" title={row.terminal.dir}>{row.terminal.dir}</bdi>
        </span>
      </button>
    </li>)}</ul>
  </div>
}
