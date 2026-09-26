import { useEffect, useState } from "react"
import type { CapacityEntry, CapacityPanel } from "@clawdline/contract"
import * as L from "../../legacy/bridge.js"
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
          if (live) setFailure(L.failureSentence(error, words("Capacity unavailable", "讀不到容量")))
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
      <b id="settings-capacity-title">{words("Capacity", "容量")}</b>
      <p className="say" id="settings-capacity-say">
        {panel
          ? summary(rows)
          : failure
            ? ""
            : words("Reading…", "讀取中…")}
      </p>
      <p className="said" id="settings-capacity-status" role="status">
        {failure && panel ? words("Showing the last reading. ", "顯示上一次讀到的。") + failure : failure}
      </p>
      {panel?.completions_error && (
        <p className="cap-warn">
          {words(
            "Completion notices could not be read, so whether any are dead letters is unknown: ",
            "完成通知讀不到，所以不知道有沒有 dead letter：",
          ) + panel.completions_error}
        </p>
      )}
      {beat && !beat.running && (
        <p className="cap-warn">
          {words(
            "The capacity patrol is not running on this daemon, so every row below is unknown.",
            "容量巡邏沒有在這個 daemon 跑，所以下面每一列都是未知。",
          )}
        </p>
      )}
      {beat?.stalled && (
        <p className="cap-warn">
          {words(
            "The capacity patrol has stalled: no pass finished in over three ticks, so the numbers below are old.",
            "容量巡邏停了：超過三輪沒有完成，下面的數字是舊的。",
          )}
        </p>
      )}
      {dead > 0 && (
        <p className="cap-warn cap-dead" title="POST /v1/orchestrator/completions/reconcile">
          {words(
            `${dead} completion notices were never taken up to the end (dead letter); each was pushed once when it happened.`,
            `${dead} 則完成通知送到最後都沒被收下（dead letter）；當時已推播過一次。`,
          )}
        </p>
      )}
      {panel && loud.length === 0 && !all && rows.length > 0 && (
        <p className="say">{words("Every row is below its warning line.", "每一列都在告警門檻以下。")}</p>
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
              ? words("Only rows that need a look", "只看要注意的")
              : words(`Show all ${rows.length} rows`, `展開全部 ${rows.length} 列`)}
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
        {row.overridden && <span>{words("Limit lowered by override", "上限被調小了")}</span>}
        {counted && <span>{counted}</span>}
      </div>
      {why && <p className="cap-note">{why}</p>}
    </li>
  )
}
