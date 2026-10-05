import { useEffect, useRef, useState } from "react"
import type { Terminal } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { readProjectPlaces, type ProjectPlace } from "../pages/work/api.js"
import { closeTerminal, hostedConsole, listTerminals, readTerminalMachine, TerminalRequestError } from "../pages/terminal/api.js"
import { TerminalInputClient } from "../pages/terminal/input-client.js"
import { fetchTransport } from "../pages/terminal/api.js"
import { openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { holderWords, terminalRefusalWords, terminalShortID, terminalStatusWords, unavailableWords } from "../pages/terminal/words.js"
import { rowNames } from "../pages/terminal/TerminalProjectList.js"
import { Start } from "./Start.js"
import { CloudAllTerminalList } from "./CloudAllTerminalList.js"
import "./terminal-list.css"

function why(error: unknown): string {
  return error instanceof TerminalRequestError ? terminalRefusalWords(error.code) : nextWord("terminalUnavailableUnknown")
}

function dirTail(dir: string | undefined): string {
  if (!dir) return ""
  return dir.length > 38 ? "…" + dir.slice(-38) : dir
}

function ProjectMark({ place }: { place?: ProjectPlace }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    if (canvas.current) L.drawIcon(canvas.current, place?.icon as L.StartPlaceRow["icon"], 4)
  }, [place?.icon])
  return <span className="session-terminal-icon" aria-hidden="true"><canvas ref={canvas} /></span>
}

/** All running terminals on this machine, using polling rather than another held stream. */
export function TerminalList({ shown, filter }: { shown: boolean; filter: string }) {
  const [rows, setRows] = useState<Terminal[]>([])
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [blocked, setBlocked] = useState("")
  const [ask, setAsk] = useState<Terminal | null>(null)
  const [closing, setClosing] = useState(false)
  const [closeError, setCloseError] = useState("")
  const dialog = useRef<HTMLDialogElement>(null)
  const closeOpener = useRef<HTMLButtonElement | null>(null)
  const generation = useRef(0)
  const refresh = useRef<() => void>(() => {})
  const hosted = hostedConsole()

  useEffect(() => {
    if (ask) dialog.current?.showModal()
  }, [ask])

  useEffect(() => {
    if (!shown || hosted) return
    let live = true
    void readProjectPlaces().then((page) => { if (live) setPlaces(page.places) }, () => undefined)
    return () => { live = false }
  }, [shown, hosted])

  useEffect(() => {
    if (!shown || hosted) return
    const mine = ++generation.current
    let pending = false
    let controller: AbortController | null = null
    const read = async () => {
      if (pending || document.hidden || mine !== generation.current) return
      pending = true
      controller = new AbortController()
      const timeout = setTimeout(() => controller?.abort(), 2500)
      try {
        const [machine, list] = await Promise.allSettled([readTerminalMachine(controller.signal), listTerminals("", TAB, controller.signal)])
        if (mine !== generation.current) return
        if (list.status === "rejected") throw list.reason
        // A paired device may list terminals while the machine-wide diagnostics
        // endpoint refuses it. The list is the authority for this device's grant.
        const unavailable = machine.status === "fulfilled" ? unavailableWords(machine.value.capability) : null
        setBlocked(unavailable ?? "")
        setRows(list.value.terminals.filter((terminal) => terminal.status === "running"))
        setError("")
      } catch (e) {
        if (mine !== generation.current) return
        const reason = why(e)
        if (e instanceof TerminalRequestError && ["terminal_forbidden", "terminal_access_revoked", "terminal_cloud_not_supported"].includes(e.code)) {
          setBlocked(reason)
          setRows([])
        } else setError(nextWord("terminalAllFailed", { why: reason }))
      } finally {
        clearTimeout(timeout)
        pending = false
        if (mine === generation.current) setLoading(false)
      }
    }
    refresh.current = () => { void read() }
    void read()
    const timer = setInterval(() => { void read() }, 2000)
    const onVisible = () => { if (!document.hidden) void read() }
    document.addEventListener("visibilitychange", onVisible)
    return () => {
      ++generation.current
      controller?.abort()
      clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisible)
    }
  }, [shown, hosted])

  const confirmClose = async () => {
    const terminal = ask
    if (!terminal || closing) return
    const index = rows.findIndex((row) => row.id === terminal.id)
    setClosing(true)
    setCloseError("")
    const input = new TerminalInputClient(terminal.id, TAB, fetchTransport)
    let succeeded = false
    try {
      const code = await input.control("acquire")
      if (code) {
        setCloseError(terminalRefusalWords(code))
        return
      }
      const epoch = input.state.epoch
      if (!input.state.holding || epoch === null) {
        setCloseError(nextWord("terminalCloseNeedsControl"))
        return
      }
      await closeTerminal(terminal.id, epoch, TAB)
      succeeded = true
      setRows((was) => was.filter((row) => row.id !== terminal.id))
      refresh.current()
    } catch (e) {
      setCloseError(why(e))
    } finally {
      await input.release()
      input.dispose()
      setClosing(false)
      if (succeeded) {
        setAsk(null)
        requestAnimationFrame(() => {
          const buttons = [...document.querySelectorAll<HTMLButtonElement>(".session-terminal-close")]
          const target = buttons[Math.min(index, buttons.length - 1)] ?? document.querySelector<HTMLButtonElement>(".session-terminal-head button")
          target?.focus()
        })
      }
    }
  }

  const cancelClose = () => {
    setAsk(null)
    setCloseError("")
    requestAnimationFrame(() => closeOpener.current?.focus())
  }

  if (hosted) return <CloudAllTerminalList shown={shown} filter={filter} />
  if (blocked) return <p className="terminal-list-message" role="note">{blocked}</p>
  const names = rowNames(rows.map((row) => row.created))
  const q = filter.trim().toLocaleLowerCase()
  const matching = rows.filter((row) => {
    const project = places.find((place) => place.id === row.project_id)
    return !q || `${project?.label ?? row.project_id} ${row.dir ?? ""}`.toLocaleLowerCase().includes(q)
  })
  return <div className="session-terminal-list">
    <div className="session-terminal-head">
      <h2>{nextWord("terminalListMode")}</h2>
      <button type="button" onClick={() => Start.openTerminal()}>{nextWord("terminalAllOpening")}</button>
    </div>
    {error && <p className="terminal-list-message" role="status">{error}</p>}
    {loading && !rows.length && <p className="terminal-list-message">{nextWord("terminalListLoading")}</p>}
    {!loading && !matching.length && <p className="terminal-list-message">{q ? L.strings.webEmptyFilterHint : nextWord("terminalAllEmpty")}</p>}
    <ul className="session-terminal-rows">
      {matching.map((terminal) => {
        const project = places.find((place) => place.id === terminal.project_id)
        const index = rows.findIndex((row) => row.id === terminal.id)
        return <li key={terminal.id}>
          <button className="session-terminal-card" type="button" onClick={() => openTerminalPage(terminal.project_id, terminal.id, "sessions")}>
            <ProjectMark place={project} />
            <span className="session-terminal-main">
              <strong>{project?.label ?? terminal.project_id}</strong>
              <span title={terminal.id}>{nextWord("terminalIdentity", { id: terminalShortID(terminal.id) })}</span>
              <span>{nextWord("terminalRowName", { time: names[index] })}</span>
              <bdi className="session-terminal-dir" title={terminal.dir}>{dirTail(terminal.dir)}</bdi>
              <span>{terminalStatusWords(terminal.status)}{terminal.control.held ? ` · ${holderWords(terminal.control.holder)}` : ""}</span>
            </span>
          </button>
          <button className="session-terminal-close" type="button" onClick={(event) => { closeOpener.current = event.currentTarget; setCloseError(""); setAsk(terminal) }} aria-label={`${nextWord("terminalClose")}: ${project?.label ?? terminal.project_id} · ${nextWord("terminalIdentity", { id: terminalShortID(terminal.id) })}`}>{nextWord("terminalClose")}</button>
        </li>
      })}
    </ul>
    {ask && <dialog ref={dialog} className="session-terminal-confirm" aria-label={nextWord("terminalCloseTarget", { project: places.find((place) => place.id === ask.project_id)?.label ?? ask.project_id, id: terminalShortID(ask.id) })} onCancel={cancelClose}>
      <h2>{nextWord("terminalCloseTarget", { project: places.find((place) => place.id === ask.project_id)?.label ?? ask.project_id, id: terminalShortID(ask.id) })}</h2>
      <p>{nextWord("terminalCloseAsk")}</p>
      {closeError && <p role="alert">{closeError}</p>}
      <button type="button" onClick={cancelClose} disabled={closing}>{nextWord("terminalCancel")}</button>
      <button type="button" onClick={() => void confirmClose()} disabled={closing}>{nextWord("terminalCloseConfirm")}</button>
    </dialog>}
  </div>
}
