import { useCallback, useEffect, useRef, useState } from "react"
import type { Terminal } from "@clawdline/contract"
import { nextWord } from "../../next-strings.js"
import {
  TerminalRequestError,
  hostedConsole,
  listTerminals,
  openTerminal,
  readTerminalMachine,
} from "./api.js"
import { openTerminalPage } from "./navigate.js"
import { TAB } from "./tab.js"
import { holderWords, terminalRefusalWords, terminalStatusWords, unavailableWords } from "./words.js"

/**
 * One project's terminals and the button that opens another: on the terminal
 * page before one is chosen, and folded open under the work page's project
 * scope.
 *
 * Its states, each drawn as itself: reading; could not read (the code's own
 * sentence, never an empty list); none yet; the rows; this machine cannot
 * open terminals at all (the reason, and no button); the console Clawdline
 * Cloud serves (one sentence, and nothing is asked of the machine).
 */
type Listing =
  | { kind: "loading" }
  | { kind: "failed"; why: string }
  | { kind: "rows"; rows: Terminal[] }

function errorWords(e: unknown): string {
  if (e instanceof TerminalRequestError) return terminalRefusalWords(e.code)
  return e instanceof Error ? e.message : String(e)
}

/** A first size for a new shell, from this window; the controller's fit corrects it at once. */
function firstSize(): { cols: number; rows: number } {
  const w = typeof window === "undefined" ? 1024 : window.innerWidth
  const h = typeof window === "undefined" ? 768 : window.innerHeight
  return {
    cols: Math.max(20, Math.min(240, Math.floor((w - 32) / 8.5))),
    rows: Math.max(8, Math.min(80, Math.floor((h - 220) / 18))),
  }
}

export function TerminalProjectList({ project, label, shown, headingLevel = 2 }: {
  project: string
  label: string
  shown: boolean
  headingLevel?: 2 | 3
}) {
  const hosted = hostedConsole()
  const [listing, setListing] = useState<Listing>({ kind: "loading" })
  const [unavailable, setUnavailable] = useState<string | null>(null)
  const [opening, setOpening] = useState(false)
  const [openFailed, setOpenFailed] = useState("")
  const ticket = useRef(0)

  const load = useCallback(async () => {
    if (hosted || !project) return
    const mine = ++ticket.current
    // refusal-ok: a machine whose diagnostics cannot be read is not called unable; the list below still refuses `terminal_unsupported` in words if it is.
    const [machine, list] = await Promise.allSettled([readTerminalMachine(), listTerminals(project, TAB)])
    if (mine !== ticket.current) return
    setUnavailable(machine.status === "fulfilled" ? unavailableWords(machine.value.capability) : null)
    if (list.status === "fulfilled") setListing({ kind: "rows", rows: list.value.terminals })
    else setListing({ kind: "failed", why: errorWords(list.reason) })
  }, [hosted, project])

  useEffect(() => {
    if (!shown) return
    setListing({ kind: "loading" })
    void load()
  }, [shown, load])

  if (hosted) return <p className="terminal-note" role="note">{nextWord("terminalRefusalCloudNotSupported")}</p>

  const Heading = headingLevel === 2 ? "h2" : "h3"
  const open = async () => {
    setOpening(true)
    setOpenFailed("")
    try {
      const size = firstSize()
      const made = await openTerminal(project, size.cols, size.rows)
      openTerminalPage(project, made.id)
    } catch (e) {
      setOpenFailed(nextWord("terminalOpenFailed", { why: errorWords(e) }))
    } finally {
      setOpening(false)
    }
  }

  return (
    <div className="terminal-list" aria-busy={listing.kind === "loading" ? "true" : undefined}>
      <div className="terminal-list-head">
        <Heading className="terminal-list-title">{nextWord("terminalListTitle", { project: label })}</Heading>
        {!unavailable && (
          <button className="board-button terminal-open-new" type="button" disabled={opening}
            aria-busy={opening ? "true" : undefined} onClick={() => void open()}>
            {opening ? nextWord("terminalOpening") : nextWord("terminalOpenNew")}
          </button>
        )}
      </div>
      {unavailable && <p className="terminal-note" role="note">{unavailable}</p>}
      <p className="terminal-status-line" role="status" aria-live="polite">{openFailed}</p>
      {listing.kind === "loading" && <p className="terminal-note">{nextWord("terminalListLoading")}</p>}
      {listing.kind === "failed" && (
        <p className="terminal-note" role="alert">{nextWord("terminalListFailed", { why: listing.why })}</p>
      )}
      {listing.kind === "rows" && listing.rows.length === 0 && !unavailable && (
        <p className="terminal-note">{nextWord("terminalListEmpty")}</p>
      )}
      {listing.kind === "rows" && listing.rows.length > 0 && (
        <ul className="terminal-rows">
          {listing.rows.map((t, i) => (
            <li key={t.id}>
              <button className="terminal-row" type="button" onClick={() => openTerminalPage(project, t.id)}>
                <span className="terminal-row-name">{nextWord("terminalRowName", { n: i + 1 })}</span>
                <span className="terminal-row-dir" title={t.dir}>{t.dir || ""}</span>
                <span className="terminal-row-facts">
                  <span data-status={t.status}>{terminalStatusWords(t.status)}</span>
                  <span>{t.cols}×{t.rows}</span>
                  <span>{nextWord("terminalControlLabel")} · {holderWords(t.control.held ? t.control.holder : null)}</span>
                  <span>{nextWord("terminalRowSince", { time: new Date(t.created * 1000).toLocaleString() })}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <p className="terminal-note terminal-session-note">{nextWord("terminalSessionNote")}</p>
    </div>
  )
}
