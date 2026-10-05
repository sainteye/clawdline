import { useEffect, useRef, useState } from "react"
import type { Terminal } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { readProjectPlaces, type ProjectPlace } from "../pages/work/api.js"
import { hostedConsole, listTerminals, readTerminalMachine, TerminalRequestError } from "../pages/terminal/api.js"
import { openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { holderWords, terminalRefusalWords, terminalShortID, terminalStatusWords, unavailableWords } from "../pages/terminal/words.js"
import { rowNames } from "../pages/terminal/TerminalProjectList.js"
import { CloudAllTerminalList } from "./CloudAllTerminalList.js"
import { TerminalMarks } from "./TerminalGlyph.js"
import "./terminal-list.css"

function why(error: unknown): string {
  return error instanceof TerminalRequestError ? terminalRefusalWords(error.code) : L.failureSentence(error, nextWord("terminalUnavailableUnknown"))
}

function dirTail(dir: string | undefined): string {
  if (!dir) return ""
  return dir.length > 38 ? "…" + dir.slice(-38) : dir
}

/**
 * All running terminals on this machine, using polling rather than another held
 * stream. A row opens its terminal in the second column, as a Session row
 * opens its conversation; opening another is the toolbar's +, and closing one
 * is in that terminal's own menu, so a row carries no buttons of its own.
 */
export function TerminalList({ shown, filter, openId = "" }: { shown: boolean; filter: string; openId?: string }) {
  const [rows, setRows] = useState<Terminal[]>([])
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [blocked, setBlocked] = useState("")
  const generation = useRef(0)
  const hosted = hostedConsole()

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
        // refusal-ok: a refused diagnostics read is deliberately not shown, because the list is the authority for this device's terminal grant.
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

  if (hosted) return <CloudAllTerminalList shown={shown} filter={filter} />
  if (blocked) return <p className="terminal-list-message" role="note">{blocked}</p>
  const names = rowNames(rows.map((row) => row.created))
  const q = filter.trim().toLocaleLowerCase()
  const matching = rows.filter((row) => {
    const project = places.find((place) => place.id === row.project_id)
    return !q || `${project?.label ?? row.project_id} ${row.dir ?? ""}`.toLocaleLowerCase().includes(q)
  })
  return <div className="session-terminal-list">
    <h2 className="terminal-sr">{nextWord("terminalListMode")}</h2>
    {error && <p className="terminal-list-message" role="status">{error}</p>}
    {loading && !rows.length && <p className="terminal-list-message">{nextWord("terminalListLoading")}</p>}
    {!loading && !matching.length && <p className="terminal-list-message">{q ? L.strings.webEmptyFilterHint : nextWord("terminalAllEmpty")}</p>}
    <ul className="session-terminal-rows">
      {matching.map((terminal) => {
        const project = places.find((place) => place.id === terminal.project_id)
        const index = rows.findIndex((row) => row.id === terminal.id)
        const open = terminal.id === openId
        return <li key={terminal.id} className={open ? "open" : undefined}>
          <button className="session-terminal-card" type="button" aria-current={open ? "true" : undefined}
            onClick={() => openTerminalPage(terminal.project_id, terminal.id)}>
            <TerminalMarks place={project} />
            <span className="session-terminal-main">
              <strong>{project?.label ?? terminal.project_id}</strong>
              <span title={terminal.id}>{nextWord("terminalIdentity", { id: terminalShortID(terminal.id) })}</span>
              <span>{nextWord("terminalRowName", { time: names[index] })}</span>
              <bdi className="session-terminal-dir" title={terminal.dir}>{dirTail(terminal.dir)}</bdi>
              <span>{terminalStatusWords(terminal.status)}{terminal.control.held ? ` · ${holderWords(terminal.control.holder)}` : ""}</span>
            </span>
          </button>
        </li>
      })}
    </ul>
  </div>
}
