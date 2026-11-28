import {useEffect, useState, type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {CloneMark} from "./BotRow"
import {dispatchedMoreLabel, dispatchedReason, dispatchedStrip} from "./dispatched-work"
import {ThinkingOrb} from "./ThinkingOrb"
import {elapsed} from "./working"
import type {Words} from "./words"
import "./dispatched-work.css"

type OpenWork = (link: W.ProjectAssistantWorkLink, view?: "session") => void

/** Whether a row leads anywhere: its worker Session, or the Board item it belongs to. */
export function dispatchedOpenable(link: W.ProjectAssistantWorkLink): boolean {
  return !!(link.worker_terminal && link.worker_session) || !!link.board_item_id
}

/** Use the same machine and conversation checks as the work result card. */
export function openDispatchedSession(link: W.ProjectAssistantWorkLink, openWork?: OpenWork) {
  openWork?.(link, link.worker_terminal && link.worker_session ? "session" : undefined)
}

/** Active dispatched work shares the line above the composer with the
 * background capsule. */
export function DispatchedWorkStrip({links, work, ownProjectID, background, onOpen, w}: {
  links: readonly W.ProjectAssistantWorkLink[]; work?: W.ProjectAssistantDispatchedWork; ownProjectID?: string; background?: ReactNode
  onOpen?: OpenWork; w: Words
}) {
  const [now, setNow] = useState(Date.now)
  const [open, setOpen] = useState(false)
  const {counts, rows: shown, more, showWorking} = dispatchedStrip(links, work)
  const ticking = shown.some(row => row.kind === "running" && row.link.started_at)
  useEffect(() => {
    if (!ticking) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [ticking])
  if (!shown.length && !more) return <>{background}</>
  const parts = [
    counts.running > 0 ? w.dispatchedStripRunning.replace("{count}", String(counts.running)) : "",
    counts.needsYou > 0 ? w.dispatchedStripNeedsYou.replace("{count}", String(counts.needsYou)) : "",
  ].filter(Boolean)
  return <div className="pa-dispatched" data-open={open || undefined}>
    <div className="pa-dispatched-line">
      {background}
      <button type="button" className="pa-dispatched-toggle" aria-expanded={open} aria-label={`${w.dispatchedStripLabel}: ${parts.join(", ")}`} onClick={() => setOpen(!open)}>
        {showWorking ? <ThinkingOrb state="working" size={13} color="currentColor"/> : <CloneMark/>}<span>{parts.map((part, i) => <span key={i} className={i === 0 && counts.running > 0 ? "pa-dispatched-lead" : undefined}>{i > 0 ? " · " : ""}{part}</span>)}</span>
        <svg className="pa-dispatched-chevron" viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ transform: open ? "rotate(90deg)" : undefined }}><path d="M6 4l4 4-4 4"/></svg>
      </button>
    </div>
    {open && <ul className="pa-dispatched-list" aria-label={w.dispatchedStripLabel}>
      {shown.map(row => {
        const project = row.link.project ?? ""
        const other = !!(ownProjectID && row.link.target_project_id && row.link.target_project_id !== ownProjectID)
        const time = row.kind === "running" ? elapsed(row.link.started_at, now) : ""
        return <li key={row.link.id}>
          {(() => {
            const openable = !!onOpen && dispatchedOpenable(row.link)
            const body = <>
              <span className="pa-dispatched-mark" aria-hidden="true"/>
              <span className="pa-dispatched-text"><strong>{row.link.title || row.link.task_id}</strong>
                <small>{dispatchedReason(row, w)}{project ? ` · ${project}` : ""}{other && <span className="pa-dispatched-other">{w.dispatchedOtherProject}</span>}
                  {!openable && <span className="pa-dispatched-unopenable"> · {w.dispatchedNoSession}</span>}</small></span>
              {time && <time>{time}</time>}
            </>
            // A row with nothing to open says so instead of being a button that does nothing.
            return openable
              ? <button type="button" className="pa-dispatched-row" data-kind={row.kind} title={row.link.landing_note || undefined} onClick={() => openDispatchedSession(row.link, onOpen)}>{body}<span aria-hidden="true">›</span></button>
              : <div className="pa-dispatched-row" data-kind={row.kind} data-unopenable="" title={row.link.landing_note || undefined}>{body}</div>
          })()}
        </li>
      })}
      {more > 0 && <li><a className="pa-dispatched-more" href="#page=sessions">{dispatchedMoreLabel(shown.length, more, w)}</a></li>}
    </ul>}
  </div>
}
