import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type MutableRefObject } from "react"
import { nextWord } from "../next-strings.js"
import { readProjectPlaces, type ProjectPlace } from "../pages/work/api.js"
import { terminalHost, watchTerminalHost, type TerminalHost } from "../cloud/terminal-host.js"
import { scheduleFleet, onScheduleFleet, type ScheduleFleet } from "../cloud/schedule-machines.js"
import { acquireTerminalConnection } from "../cloud/terminal-connection-owner.js"
import { TerminalConnectionsDialog } from "../pages/terminal/TerminalConnectionsDialog.js"
import { TerminalObservation } from "../cloud/terminal-observation.js"
import { openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { holderWords, terminalRefusalWords, terminalShortID, terminalStatusWords } from "../pages/terminal/words.js"
import { collectCloudTerminals, readCloudTerminalList, terminalListNotices, withTerminalListRetries, CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS,
  type CloudTerminalRow, type CloudTerminalError } from "./cloud-terminal-all.js"
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

/**
 * The hosted tty list reads the machine this console is connected to, over
 * that machine's own encrypted terminal channel. Other paired machines are a
 * switch away in the header, as their Sessions are; listing them here too put
 * terminals beside a Session list that could not open them. Reading again is a
 * pull on the list, so the list carries no controls of its own.
 */
export function CloudAllTerminalList({ shown, filter, reloadRef }: {
  shown: boolean; filter: string; reloadRef?: MutableRefObject<(() => Promise<void>) | null>
}) {
  const [host, setHost] = useState<TerminalHost | null>(terminalHost)
  const [fleet, setFleet] = useState<ScheduleFleet | null>(scheduleFleet)
  const places = useRef<ProjectPlace[]>([])
  const [rows, setRows] = useState<CloudTerminalRow[]>([])
  const [errors, setErrors] = useState<CloudTerminalError[]>([])
  const connectionsButton = useRef<HTMLButtonElement>(null)
  const [connectionsOpen, setConnectionsOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [projectError, setProjectError] = useState("")
  const [routeError, setRouteError] = useState("")
  const [refresh, setRefresh] = useState(0)
  // The automatic re-read in progress (1-based), or 0. A machine that could not
  // verify this browser yet answers terminal_busy; the list asks again by itself.
  const [retrying, setRetrying] = useState(0)
  useSyncExternalStore(watchTerminalClose, terminalCloseRevision, () => 0)
  const latestClose = recentTerminalCloseStates()[0]
  const generation = useRef(0)
  const currentRows = useRef(rows)
  currentRows.current = rows

  useEffect(() => watchTerminalHost(setHost), [])
  useEffect(() => {
    if (shown && errors.some((error) => error.code === "terminal_connections_full")) setConnectionsOpen(true)
  }, [shown, errors])
  useEffect(() => onScheduleFleet(() => setFleet(scheduleFleet())), [])
  const machineName = fleet?.machines.find((machine) => machine.id === host?.machine)?.name ?? ""
  const reload = useCallback(async () => {
    if (!host) return
    const mine = ++generation.current
    setLoading(true)
    setErrors([])
    setRetrying(0)
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
      const machines = [{ id: host.machine, name: machineName }]
      const next = await withTerminalListRetries(() => collectCloudTerminals(machines, nextPlaces, currentRows.current,
        (machine) => readMachine(host, machine),
        (progress) => { if (mine === generation.current) { setRows(progress.rows); setErrors(progress.errors) } }),
      ({ attempt, result }) => {
        if (mine !== generation.current) return
        setRows(result.rows); setErrors(result.errors); setRetrying(attempt)
      }, () => mine === generation.current)
      if (mine === generation.current) { setRows(next.rows); setErrors(next.errors) }
    } finally {
      if (mine === generation.current) setLoading(false)
    }
  }, [host, machineName, refresh])
  useEffect(() => {
    if (!shown) return
    void reload()
    return () => { ++generation.current }
  }, [shown, reload])
  const retry = () => setRefresh((value) => value + 1)
  if (reloadRef) reloadRef.current = reload
  const repairPairing = (machine: string) => {
    if (openMachinePairing(machine)) return
    const row = document.querySelector<HTMLButtonElement>('#sidebar [data-page-to="devices"]')
    if (row && !row.disabled) row.click()
  }

  const query = filter.trim().toLocaleLowerCase()
  const projectOptions = [...new Map(rows.filter((row) => row.project).map((row) => [row.project, row.projectName])).entries()]
  const matching = rows.filter((row) => {
    const close = terminalCloseState(row.machine, row.terminal.id)
    return close?.status !== "ok" && close?.status !== "ended" && row.machine === host?.machine && (!query ||
      `${row.projectName} ${row.machineName} ${row.terminal.id} ${row.terminal.dir ?? ""}`.toLocaleLowerCase().includes(query))
  })
  const open = (row: CloudTerminalRow) => {
    if (!row.project) { setRouteError(nextWord("terminalAllUnknownProject")); return }
    openTerminalPage(row.project, row.terminal.id)
  }

  if (!host) return <div className="session-terminal-list" aria-busy="true">
    <p className="sr-only" role="status">{nextWord("terminalCloudLineReconnecting")}</p><TerminalRowsSkeleton />
  </div>
  return <div className="session-terminal-list" aria-busy={loading ? "true" : undefined}>
    {connectionsOpen && <TerminalConnectionsDialog host={host} machine={host.machine} focusTarget={connectionsButton.current} close={() => setConnectionsOpen(false)} />}
    <button ref={connectionsButton} className="board-button terminal-connections-open" type="button" onClick={() => setConnectionsOpen(true)}>{nextWord("terminalConnectionsTitle")}</button>
    <h2 className="terminal-sr">{nextWord("terminalListMode")}</h2>
    {routeError && <p className="terminal-list-message" role="alert">{routeError}</p>}
    {latestClose && <p className="terminal-list-message" role="status">
      {nextWord("terminalIdentity", { id: terminalShortID(latestClose.terminal) })} · {nextWord(latestClose.status === "ok" ? "terminalTerminateSucceeded" :
        latestClose.status === "ended" ? "terminalTerminateEnded" : latestClose.status === "pending" ? "terminalTerminatePending" :
          latestClose.status === "unknown" ? "terminalTerminateUnknown" : "terminalCloudError",
        { code: terminalRefusalWords(latestClose.error) })}</p>}
    {terminalListNotices(projectError, errors, host.machine, loading && retrying > 0).map((notice) => {
      const machineName = fleet?.machines.find((machine) => machine.id === notice.machine)?.name || notice.machine
      const checking = notice.kind === "checking"
      return <section className="terminal-list-notice" data-tone={notice.kind === "offline" || checking ? "warn" : "bad"}
        role={checking ? "status" : "alert"} key={notice.key}>
        <strong>{notice.kind === "offline" ? nextWord("terminalNoticeOfflineTitle", { machine: machineName }) :
          notice.kind === "denied" ? nextWord("terminalNoticeDeniedTitle", { machine: machineName }) :
          checking || notice.kind === "unverified" ? nextWord("terminalNoticeCheckingTitle", { machine: machineName }) :
          notice.kind === "projects" ? nextWord("terminalNoticeProjectsTitle") : nextWord("terminalNoticeFailedTitle", { machine: machineName })}</strong>
        <p>{notice.kind === "offline" ? nextWord("terminalNoticeOfflineBody") : notice.kind === "denied" ? nextWord("terminalNoticeDeniedBody") :
          checking ? nextWord("terminalNoticeCheckingBody", { attempt: String(retrying), max: String(CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS.length) }) :
          notice.kind === "unverified" ? nextWord("terminalNoticeUnverifiedBody") : terminalRefusalWords(notice.code) !== notice.code ? terminalRefusalWords(notice.code) : nextWord("terminalNoticeFailedBody")}</p>
        <div className="terminal-list-notice-foot">
          <code>{notice.code}</code>
          {!checking && (notice.kind === "denied"
            ? <button type="button" onClick={() => repairPairing(notice.machine)}>{nextWord("terminalAllRepairPairing")}</button>
            : <button type="button" onClick={retry} disabled={loading}>{nextWord("terminalAllRetry")}</button>)}
        </div>
      </section>
    })}
    {loading && <p className="sr-only" role="status">{nextWord("terminalListLoading")}</p>}
    {loading && !rows.length && <TerminalRowsSkeleton />}
    {!loading && !projectError && !matching.length && query &&
      <p className="terminal-list-message">{nextWord("terminalAllEmptyFilter")}</p>}
    {!loading && !projectError && !errors.length && !matching.length && !query &&
      <p className="terminal-list-message">{nextWord("terminalAllCloudEmpty")}</p>}
    <ul className="session-terminal-rows">{matching.map((row) => <li key={`${row.machine}/${row.terminal.id}`}>
      <button className="session-terminal-card" type="button" onClick={() => open(row)}
        aria-label={nextWord("terminalOpenView", { project: row.projectName, machine: row.machineName, id: terminalShortID(row.terminal.id) })}>
        <TerminalMarks place={places.current.find((place) => place.id === row.project)} />
        <span className="session-terminal-main">
          <strong>{row.projectName}</strong>
          <span>{row.machineName ? `${row.machineName} · ` : ""}{nextWord("terminalIdentity", { id: terminalShortID(row.terminal.id) })}</span>
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

/**
 * The list before its first answer: rows in the cards' own frame (`.session-terminal-rows`, the
 * marks column, three lines and the folder), so the real rows take their places without a jump.
 * The words for a screen reader are said apart, in the status line above it.
 */
function TerminalRowsSkeleton() {
  return <ul className="session-terminal-rows terminal-list-skeleton" aria-hidden="true">{[0, 1, 2].map((index) => <li key={index}>
    <span className="session-terminal-card">
      <span className="session-terminal-marks"><span className="terminal-skel terminal-skel-glyph" /></span>
      <span className="session-terminal-main terminal-skel-main">
        <span className="terminal-skel terminal-skel-title" />
        <span className="terminal-skel terminal-skel-line" />
        <span className="terminal-skel terminal-skel-line terminal-skel-short" />
        <span className="terminal-skel terminal-skel-line terminal-skel-dir" />
      </span>
    </span>
  </li>)}</ul>
}
