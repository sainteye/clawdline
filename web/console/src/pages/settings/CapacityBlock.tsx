import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { useEffect, useState } from "react"
import type { CapacityEntry, CapacityPanel } from "@clawdline/contract"
import * as L from "../../legacy/bridge.js"
import { headerReadDiagnostics } from "../../cloud/relay-reader.js"
import {
  bySeverity,
  counters,
  evicts,
  fullBy,
  lastAlert,
  readCapacity,
  reading,
  stateWord,
  summary,
  words,
} from "./capacity.js"
import "./capacity.css"

/** How often an open Settings page reads the register again. */
const POLL_MS = 15_000

/**
 * The settings page's capacity block: how full every bounded thing the daemon
 * keeps is (docs/limits.md §4.5, design-decisions C4), and the completion
 * notices that went unanswered. Every capacity push ends by naming this block,
 * so it is where a person who got one on a phone lands.
 *
 * It was the Dashboard's panel until that page was removed on 2026-09-21; the
 * push kept pointing at it. What it keeps from there:
 *
 * - a row that is not ok is always listed, fullest first; the rest are one
 *   press away;
 * - a row that could not be measured says so and draws no bar — unknown is
 *   not empty;
 * - dead letters are shown, since they are pushed only once;
 * - a patrol that is not running, or has stalled, is said above the rows,
 *   because then every number under it is old or absent.
 *
 * What is different: it uses the sheet's furniture and a column that wraps at
 * phone width rather than the Dashboard's one-line rows, and it reads through
 * `capacity.ts`, which Clawdline Cloud carries, rather than a URL only this
 * machine's browser could reach. A refusal is said in the words every other
 * block uses (`failureSentence`). It reads while the page is shown and stops
 * when it is not.
 */
export function CapacityBlock({ shown }: { shown: boolean }) {
  const [panel, setPanel] = useState<CapacityPanel | null>(null)
  const [failure, setFailure] = useState("")
  const [all, setAll] = useState(false)
  const [diagnosticStatus, setDiagnosticStatus] = useState("")

  const copyHeaderDiagnostics = async () => {
    const rows = headerReadDiagnostics()
    if (!rows.length) { setDiagnosticStatus(catalogWord("literal", "712dbdd9c3f0")); return }
    try {
      await navigator.clipboard.writeText(JSON.stringify(rows, null, 2))
      setDiagnosticStatus(catalogWord("literal", "be28f5f7d1a9"))
    } catch {
      // refusal-ok: a clipboard write is refused by the browser's permission, which carries no machine code to name.
      setDiagnosticStatus(catalogWord("literal", "ec06934bf3cc"))
    }
  }

  useEffect(() => {
    if (!shown) return
    let live = true
    const read = () =>
      readCapacity()
        .then((next) => {
          if (!live) return
          setPanel(next)
          setFailure("")
        })
        .catch((error: unknown) => {
          if (live) setFailure(L.failureSentence(error, catalogWord("literal", "aff43bfb749b")))
        })
    void read()
    const timer = setInterval(read, POLL_MS)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [shown])

  const rows = panel ? [...panel.capacity.entries].sort(bySeverity) : []
  const loud = rows.filter((r) => r.state !== "ok")
  const listed = all ? rows : loud
  const beat = panel?.capacity.beat
  const dead = panel?.completions?.dead_letter ?? 0

  return (
    <div className="block settings-capacity" id="settings-capacity">
      <b id="settings-capacity-title">{catalogWord("literal", "6ea9f84cc7ae")}</b>
      <div className="row">
        <button className="chip" type="button" onClick={() => void copyHeaderDiagnostics()}>
          {catalogWord("literal", "e47d3117e261")}
        </button>
        <span role="status">{diagnosticStatus}</span>
      </div>
      <p className="say" id="settings-capacity-say">
        {panel
          ? summary(rows)
          : failure
            ? ""
            : catalogWord("literal", "51062dc6381b")}
      </p>
      <p className="said" id="settings-capacity-status" role="status">
        {failure && panel ? catalogWord("literal", "6f684cb2905c") + failure : failure}
      </p>
      {panel?.completions_error && (
        <p className="cap-warn">
          {catalogWord("literal", "7331776e6ff4") + panel.completions_error}
        </p>
      )}
      {beat && !beat.running && (
        <p className="cap-warn">
          {catalogWord("literal", "4c7b4ddb78d8")}
        </p>
      )}
      {beat?.stalled && (
        <p className="cap-warn">
          {catalogWord("literal", "2f93135491a8")}
        </p>
      )}
      {dead > 0 && (
        <p className="cap-warn cap-dead" title={catalogWord("inline", "c34776cd2efc")}>
          {words(
            `${dead} completion notices were never taken up to the end (dead letter); each was pushed once when it happened.`,
            catalogFormat("template", "d4bddd9e5d03", [dead]),
          )}
        </p>
      )}
      {panel && loud.length === 0 && !all && rows.length > 0 && (
        <p className="say">{catalogWord("literal", "1a2fd5039544")}</p>
      )}
      {listed.length > 0 && (
        <ul className="cap-rows">
          {listed.map((r) => (
            <CapacityRow key={r.name} row={r} />
          ))}
        </ul>
      )}
      {panel && rows.length > loud.length && (
        <div className="row">
          <button className="chip" id="settings-capacity-all" type="button" aria-expanded={all} onClick={() => setAll((v) => !v)}>
            {all
              ? catalogWord("literal", "73576f2cee8d")
              : words(`Show all ${rows.length} rows`, catalogFormat("template", "6b7df3d81439", [rows.length]))}
          </button>
        </div>
      )}
    </div>
  )
}

function CapacityRow({ row }: { row: CapacityEntry }) {
  const pct = row.ratio == null ? null : Math.min(100, Math.floor(row.ratio * 100))
  const counted = counters(row)
  const why = row.error ?? row.note
  return (
    <li className="cap" data-state={row.state}>
      <div className="cap-line">
        <span className="cap-name" title={row.class}>
          {row.name}
        </span>
        <span className="cap-state">{stateWord(row.state)}</span>
        <span className="cap-amount">{reading(row)}</span>
      </div>
      <div className="cap-meter" aria-hidden="true">
        {pct != null && <i style={{ width: `${pct}%` }} />}
      </div>
      <div className="cap-meta">
        <span>{evicts(row)}</span>
        <span>{lastAlert(row)}</span>
        {row.projected_full_at ? <span>{fullBy(row.projected_full_at)}</span> : null}
        {row.overridden && <span>{catalogWord("literal", "0bd13da58a88")}</span>}
        {counted && <span>{counted}</span>}
      </div>
      {why && <p className="cap-note">{why}</p>}
    </li>
  )
}
