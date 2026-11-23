import {useEffect, useState, type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {CloneMark} from "./BotRow"
import {dispatchedCounts, dispatchedReason, dispatchedRows, DispatchedStripRows} from "./dispatched-work"
import {elapsed} from "./working"
import type {Words} from "./words"
import "./dispatched-work.css"

type OpenWork = (link: W.ProjectAssistantWorkLink, view?: "session") => void

/** Use the same machine and conversation checks as the work result card. */
export function openDispatchedSession(link: W.ProjectAssistantWorkLink, openWork?: OpenWork) {
  openWork?.(link, link.worker_terminal && link.worker_session ? "session" : undefined)
}

/** "N 個工作在跑" above the Bot's composer, sharing the line with the
 * background capsule; it takes no space when nothing is dispatched. */
export function DispatchedWorkStrip({links, ownProjectID, background, onOpen, w}: {
  links: readonly W.ProjectAssistantWorkLink[]; ownProjectID?: string; background?: ReactNode
  onOpen?: OpenWork; w: Words
}) {
  const [now, setNow] = useState(Date.now)
  const [open, setOpen] = useState(false)
  const rows = dispatchedRows(links), counts = dispatchedCounts(rows)
  const ticking = rows.some(row => row.kind === "running" && row.link.started_at)
  useEffect(() => {
    if (!ticking) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [ticking])
  if (!rows.length) return <>{background}</>
  const parts = [
    counts.running > 0 ? w.dispatchedStripRunning.replace("{count}", String(counts.running)) : "",
    counts.needsYou > 0 ? w.dispatchedStripNeedsYou.replace("{count}", String(counts.needsYou)) : "",
    counts.delivered > 0 ? w.dispatchedStripDelivered.replace("{count}", String(counts.delivered)) : "",
  ].filter(Boolean)
  const shown = rows.slice(0, DispatchedStripRows), more = rows.length - shown.length
  return <div className="pa-dispatched" data-open={open || undefined}>
    <div className="pa-dispatched-line">
      {background}
      <button type="button" className="pa-dispatched-toggle" aria-expanded={open} aria-label={`${w.dispatchedStripLabel}: ${parts.join(", ")}`} onClick={() => setOpen(!open)}>
        <CloneMark/><span>{parts.map((part, i) => <span key={i} className={i === 0 && counts.running > 0 ? "pa-dispatched-lead" : undefined}>{i > 0 ? " · " : ""}{part}</span>)}</span>
        <span className="pa-dispatched-chevron" aria-hidden="true">{open ? "▾" : "▸"}</span>
      </button>
    </div>
    {open && <ul className="pa-dispatched-list" aria-label={w.dispatchedStripLabel}>
      {shown.map(row => {
        const project = row.link.project ?? ""
        const other = !!(ownProjectID && row.link.target_project_id && row.link.target_project_id !== ownProjectID)
        const time = row.kind === "running" ? elapsed(row.link.started_at, now) : ""
        return <li key={row.link.id}>
          <button type="button" className="pa-dispatched-row" data-kind={row.kind} title={row.link.landing_note || undefined} onClick={() => openDispatchedSession(row.link, onOpen)}>
            <span className="pa-dispatched-mark" aria-hidden="true"/>
            <span className="pa-dispatched-text"><strong>{row.link.title || row.link.task_id}</strong>
              <small>{dispatchedReason(row, w)}{project ? ` · ${project}` : ""}{other && <span className="pa-dispatched-other">{w.dispatchedOtherProject}</span>}</small></span>
            {time && <time>{time}</time>}<span aria-hidden="true">›</span>
          </button>
        </li>
      })}
      {more > 0 && <li><a className="pa-dispatched-more" href="#page=sessions">{w.dispatchedStripMore.replace("{count}", String(more))}</a></li>}
    </ul>}
  </div>
}
