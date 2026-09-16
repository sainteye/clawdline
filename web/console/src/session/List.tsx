import { useEffect, useLayoutEffect, useRef, useState } from "react"
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
 * Background shells go last in every branch, as `shellsSaid` does there: a
 * session can be working on one thing with a build it started three turns ago
 * still going, and an idle one with a build going is the row this is for.
 *
 * Peer waits are absent rather than empty: this daemon has no reading for
 * them, and the piece is simply not there.
 */
function stateHTML(row: SessionRow): string {
  const T = L.strings
  const work = L.workState(row)
  let said = L.workStateHTML(row)
  if (L.closeability(row).block) said += L.closeabilityHTML(row)
  said += shellsHTML(row)

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

/** What the session left running where nobody can see it — `shellsSaid` in `list.js`. */
function shellsHTML(row: SessionRow): string {
  const n = row.shells?.length ?? 0
  if (!n) return ""
  const T = L.strings
  const said = n === 1 ? T.sessionShellOne : L.fillString(T.sessionShellMany, { n })
  return `<span class="shells">${L.escapeHTML(said)}</span>`
}

/** The catalog is trusted, but it goes into markup, so it is escaped anyway. */
function escapeText(text: string): string {
  return String(text ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string,
  )
}

/**
 * The spinner is drawn here, once, when its markup is written — not left to
 * the clock, which does not run while the page is hidden (see
 * `L.paintSpinner`). A layout effect, so the canvas has its size before the
 * row is painted. React rewrites the markup only when the string changes, and
 * only then is there a new, undrawn canvas.
 */
function StateLine({ row }: { row: SessionRow }) {
  const ref = useRef<HTMLDivElement>(null)
  const html = stateHTML(row)
  useLayoutEffect(() => {
    L.paintSpinner(ref.current?.querySelector<HTMLCanvasElement>("canvas.spin") ?? null)
  }, [html])
  // The live line is written separately from the markup, as `list.js` does
  // with setText. It changes every second while a session works, and putting
  // it in the markup would rebuild the spinner's canvas each time; kept out,
  // the markup changes only when the shape of the line does.
  useLayoutEffect(() => {
    const line = ref.current?.querySelector<HTMLElement>(".line")
    if (line && line.textContent !== (row.line ?? "")) line.textContent = row.line ?? ""
  }, [html, row.line])
  return <div className="state" ref={ref} dangerouslySetInnerHTML={{ __html: html }} />
}

/**
 * One row. The highlight and the open session are two things: arrows move
 * `.selected` without opening anything, and a session can stay open while the
 * highlight is elsewhere. A press opens.
 */
export function Row({
  row,
  selected,
  open,
  onOpen,
}: {
  row: SessionRow
  selected: boolean
  open: boolean
  onOpen: (id: string) => void
}) {
  // From the decorated copy the copied modules hold, so the machine identity
  // the original supplies client-side is the one this reads.
  const machine = L.machineFor(row)
  const who = L.whoHTML(row.assistant)
  // `fillRow` turns the two classes on and off on the node it already has, so
  // they stand in the order they were last turned on: a row that was open and
  // selected, lost the highlight and got it back reads `row open selected`.
  // React writes `row` once and leaves the attribute to this.
  const ref = useRef<HTMLLIElement>(null)
  useLayoutEffect(() => {
    const node = ref.current
    if (!node) return
    node.classList.toggle("selected", selected)
    node.classList.toggle("open", open)
  }, [selected, open])
  return (
    <li
      ref={ref}
      className="row"
      role="option"
      tabIndex={-1}
      data-id={row.id}
      data-selection-key={L.selectionKey(row)}
      data-state={row.state}
      aria-selected={selected ? "true" : "false"}
      aria-disabled="false"
      onClick={() => onOpen(row.id)}
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
      {/* The phone's swipe-to-end control, in `buildRow`'s markup. Nothing here
          reveals it yet, so it stays hidden as the original starts it. */}
      <button className="swipe-end" type="button" hidden>
        {L.strings.webEndSession}
      </button>
    </li>
  )
}

