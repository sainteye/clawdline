import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react"
import { nextWord } from "../next-strings.js"
import { readProjectPlaces, type ProjectPlace } from "../pages/work/api.js"
import { terminalHost, watchTerminalHost, type TerminalHost } from "../cloud/terminal-host.js"
import { scheduleFleet, onScheduleFleet, type ScheduleFleet } from "../cloud/schedule-machines.js"
import { TerminalChannelTransport } from "../cloud/terminal-transport.js"
import { CloudTerminalSession } from "../cloud/terminal-session.js"
import { openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { holderWords, terminalRefusalWords, terminalShortID, terminalStatusWords } from "../pages/terminal/words.js"
import { collectCloudTerminals, type CloudTerminalRow, type CloudTerminalError } from "./cloud-terminal-all.js"
import { recentTerminalCloseStates, terminalCloseRevision, terminalCloseState, watchTerminalClose } from "../cloud/terminal-close-state.js"

async function readMachine(host: TerminalHost, machine: string): Promise<import("@clawdline/contract").Terminal[]> {
  const transport = new TerminalChannelTransport(host.client, machine)
  const session = new CloudTerminalSession(transport, TAB)
  try {
    await session.start()
    const answer = await session.request("list", { client: TAB })
    const rows = answer.result?.terminals
    if (!Array.isArray(rows)) throw Object.assign(new Error("terminal_bad_receipt"), { code: "terminal_bad_receipt" })
    return rows as import("@clawdline/contract").Terminal[]
  } finally {
    session.dispose()
    transport.dispose()
  }
}

/** The hosted tty list uses each paired machine's own encrypted terminal channel. */
export function CloudAllTerminalList({ shown, filter }: { shown: boolean; filter: string }) {
  const [host, setHost] = useState<TerminalHost | null>(terminalHost)
  const [fleet, setFleet] = useState<ScheduleFleet | null>(scheduleFleet)
  const [places, setPlaces] = useState<ProjectPlace[] | null>(null)
  const [rows, setRows] = useState<CloudTerminalRow[]>([])
  const [errors, setErrors] = useState<CloudTerminalError[]>([])
  const [loading, setLoading] = useState(true)
  const [projectError, setProjectError] = useState("")
  const [routeError, setRouteError] = useState("")
  const [machineFilter, setMachineFilter] = useState("")
  const [projectFilter, setProjectFilter] = useState("")
  useSyncExternalStore(watchTerminalClose, terminalCloseRevision, () => 0)
  const latestClose = recentTerminalCloseStates()[0]
  const generation = useRef(0)
  const currentRows = useRef(rows)
  currentRows.current = rows

  useEffect(() => watchTerminalHost(setHost), [])
  useEffect(() => onScheduleFleet(() => setFleet(scheduleFleet())), [])
  useEffect(() => {
    if (!shown) return
    let live = true
    void readProjectPlaces().then((page) => { if (live) { setPlaces(page.places); setProjectError("") } },
      (error) => { if (live) setProjectError(error instanceof Error ? error.message : "project_list_failed") })
    return () => { live = false }
  }, [shown, host])

  const fleetKey = JSON.stringify(fleet?.machines.map((machine) => [machine.id, machine.name]) ?? [])
  const reload = useCallback(async () => {
    if (!host || !fleet || !places) return
    const mine = ++generation.current
    setLoading(true)
    try {
      const machines = fleet.machines.map((machine) => ({ id: machine.id, name: machine.name }))
      const next = await collectCloudTerminals(machines, places, currentRows.current,
        (machine) => readMachine(host, machine))
      if (mine === generation.current) { setRows(next.rows); setErrors(next.errors) }
    } finally {
      if (mine === generation.current) setLoading(false)
    }
  }, [host, fleetKey, places])
  useEffect(() => {
    if (!shown) return
    void reload()
    return () => { ++generation.current }
  }, [shown, reload])

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
    <div className="session-terminal-head"><h2>{nextWord("terminalListMode")} · {nextWord("terminalListCount", { n: matching.length })}</h2>
      <button type="button" onClick={() => void reload()} disabled={loading}>{nextWord("terminalCloudReloadList")}</button></div>
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
    {projectError && <p className="terminal-list-message" role="alert">{nextWord("terminalProjectsFailedCode", { code: projectError })}</p>}
    {routeError && <p className="terminal-list-message" role="alert">{routeError}</p>}
    {latestClose && <p className="terminal-list-message" role="status">
      {nextWord("terminalIdentity", { id: terminalShortID(latestClose.terminal) })} · {nextWord(latestClose.status === "ok" ? "terminalTerminateSucceeded" :
        latestClose.status === "ended" ? "terminalTerminateEnded" : latestClose.status === "pending" ? "terminalTerminatePending" :
          latestClose.status === "unknown" ? "terminalTerminateUnknown" : "terminalCloudError",
        { code: terminalRefusalWords(latestClose.error) })}</p>}
    {errors.map((error) => <p className="terminal-list-message" role="alert" key={error.machine}>
      {nextWord("terminalAllMachineFailed", { machine: fleet?.machines.find((machine) => machine.id === error.machine)?.name ?? error.machine,
        why: error.code })}</p>)}
    {loading && !rows.length && <p className="terminal-list-message" role="status">{nextWord("terminalListLoading")}</p>}
    {!loading && !projectError && !matching.length && (query || machineFilter || projectFilter) &&
      <p className="terminal-list-message">{nextWord("terminalAllEmptyFilter")}</p>}
    {!loading && !projectError && !errors.length && !matching.length && !query && !machineFilter && !projectFilter &&
      <p className="terminal-list-message">{nextWord("terminalAllCloudEmpty")}</p>}
    <ul className="session-terminal-rows">{matching.map((row) => <li key={`${row.machine}/${row.terminal.id}`}>
      <button className="session-terminal-card" type="button" onClick={() => open(row)}
        aria-label={nextWord("terminalOpenView", { project: row.projectName, machine: row.machineName, id: terminalShortID(row.terminal.id) })}>
        <span className="session-terminal-main">
          <strong>{row.projectName}</strong>
          <span>{row.machineName} · {nextWord("terminalIdentity", { id: terminalShortID(row.terminal.id) })}</span>
          <span>{terminalStatusWords(row.terminal.status)} · {holderWords(row.terminal.control.held ? row.terminal.control.holder : null)}</span>
          <span>{nextWord("terminalScreenUnverified")}</span>
          {terminalCloseState(row.machine, row.terminal.id)?.status === "unknown" && <span>{nextWord("terminalTerminateUnknown")}</span>}
          {terminalCloseState(row.machine, row.terminal.id)?.status === "pending" && <span>{nextWord("terminalTerminatePending")}</span>}
          {row.stale && <span>{nextWord("terminalAllLastConfirmed", { time: new Date(row.confirmedAt).toLocaleTimeString() })}</span>}
          <bdi className="session-terminal-dir" title={row.terminal.dir}>{row.terminal.dir}</bdi>
        </span>
      </button>
      <button className="session-terminal-close" type="button" onClick={() => open(row)}>{nextWord("terminalTerminateOpen")}</button>
    </li>)}</ul>
  </div>
}
