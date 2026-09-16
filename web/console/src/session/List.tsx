import { useEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"

export function Mark({ icon, cellPx, id }: { icon: SessionRow["icon"]; cellPx: number; id?: string }) {
  const ref = useRef<HTMLCanvasElement>(null)
  const [none, setNone] = useState(false)
  useEffect(() => {
    setNone(!L.paintIcon(ref.current, icon, cellPx))
  }, [icon, cellPx])
  return <canvas className={none ? "mark none" : "mark"} id={id} ref={ref} width={0} height={0} />
}

/**
 * The line under a row's path.
 *
 * Built as one HTML string and set on `.state`, which is what `list.js` does —
 * and the reason is not style. `.state` is a flex row and the pieces the copied
 * modules return are meant to be its direct children; wrapping each one in a
 * span of its own, which is what JSX does, gave them a parent nobody styled and
 * made the row 219 pixels tall instead of 86.
 *
 * The branches and their order are that file's, because the order is the
 * precedence: a session that stopped to ask leads with the request whatever
 * else is true of it, and a screen that could not be read says so rather than
 * being drawn as idle — an idle row reads as finished, which is a confident
 * wrong answer about somebody's work.
 *
 * Peer waits and background shells are absent rather than empty: this daemon
 * has no reading for either, and the pieces are simply not there.
 */
function stateHTML(row: SessionRow): string {
  const T = L.strings
  const work = L.workState(row)
  let said = L.workStateHTML(row)
  if (L.closeability(row).block) said += L.closeabilityHTML(row)

  if (work.state === "waiting_you") {
    return `<span class="wants">${L.glyphHTML("🙋", T.sessionWaiting)}</span>${said}`
  }
  if (work.state === "working") {
    return `<canvas class="spin"></canvas><span class="line"></span>${said}`
  }
  if (work.state === "unknown" && row.state === "unknown") {
    return `<span class="unread">${escapeText(T.webStateUnreadable)}</span>${said}`
  }
  return said
}

/** The catalog is trusted, but it goes into markup, so it is escaped anyway. */
function escapeText(text: string): string {
  return String(text ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string,
  )
}

function StateLine({ row }: { row: SessionRow }) {
  return <div className="state" dangerouslySetInnerHTML={{ __html: stateHTML(row) }} />
}

export function Row({
  row,
  open,
  onSelect,
}: {
  row: SessionRow
  open: boolean
  onSelect: (id: string) => void
}) {
  // From the decorated copy the copied modules hold, so the machine identity
  // the original supplies client-side is the one this reads.
  const machine = L.machineFor(row)
  const who = L.whoHTML(row.assistant)
  return (
    <li
      className={open ? "row open selected" : "row"}
      role="option"
      aria-selected={open}
      tabIndex={-1}
      data-state={row.state}
      onClick={() => onSelect(row.id)}
    >
      <span className="kid" hidden aria-hidden="true">
        └
      </span>
      <Mark icon={row.icon} cellPx={4} />
      <div className="title" style={{ color: L.accentTint(row.icon?.accent) }}>
        <span className="label">{row.label || row.tty || row.id}</span>
        <span className="who" hidden={!who} dangerouslySetInnerHTML={{ __html: who }} />
      </div>
      <div className="meta">
        <span className="machine" title={machine.id} data-kind={machine.kind}>
          {machine.label}
        </span>
        <span className="path">{L.path(row.cwd)}</span>
        <span className="tty">{row.tty || row.backend || ""}</span>
        <span className="agents-chip" hidden>
          <span className="dot" />
          <span className="n" />
        </span>
        <span className="task-chip" hidden />
      </div>
      <StateLine row={row} />
    </li>
  )
}

