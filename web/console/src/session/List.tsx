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
 * The line under a row's path, and the key it is rebuilt on.
 *
 * Built as one HTML string and set on `.state`, which is what `list.js` does —
 * and the reason is not style. `.state` is a flex row and the pieces the copied
 * modules return are meant to be its direct children; wrapping each one in a
 * span of its own, which is what JSX does, gave them a parent nobody styled and
 * made the row 219 pixels tall instead of 86.
 *
 * The branches and their order are `fillRow`'s, because the order is the
 * precedence: a session that stopped to ask leads with the request whatever
 * else is true of it, and a screen that could not be read says so rather than
 * being drawn as idle — an idle row reads as finished, which is a confident
 * wrong answer about somebody's work.
 *
 * A peer wait (`coordination`) goes first after the state's own piece: it is
 * an overlay on the state, never the state. Background shells go last in every
 * branch, as `shellsSaid` does there.
 *
 * `shape` is the original's `data-shape`: what kind of line this is, compared
 * with the last one drawn. It is written on the element as the original writes
 * it, and it is also what decides whether the markup is rebuilt.
 *
 * This console has no optimistic sends and no in-flight close, so `kind` is the
 * row's own state: the original's `closing` and `pending` branches never apply.
 */
function stateLine(row: SessionRow): { html: string; shape: string } {
  const T = L.strings
  const coordination = row.coordination
  const waitingOn = coordination?.waitingOn ?? []
  const waitedOnBy = coordination?.waitedOnBy ?? []
  const roots = L.tasksOfRoot(row.id)
  const n = row.shells?.length ?? 0
  const shellsSaid = n
    ? `<span class="shells">${L.escapeHTML(n === 1 ? T.sessionShellOne : L.fillString(T.sessionShellMany, { n }))}</span>`
    : ""

  const peerWait = waitingOn[0] ?? null
  const owedWait = waitedOnBy[0] ?? null
  let owedSaid = ""
  if (owedWait) {
    owedSaid = waitedOnBy.length === 1 ? T.sessionWaitedOnByOne : L.fillString(T.sessionWaitedOnByMany, { n: waitedOnBy.length })
  }
  let peerText = ""
  let peerTitle = ""
  if (peerWait) {
    const owner = peerWait.ownerLabel || peerWait.ownerSessionId || "Clawdline"
    peerText = owner + " · " + (peerWait.releaseCondition || "release")
    if (waitingOn.length > 1) peerText += "  +" + String(waitingOn.length - 1)
    if (owedWait) peerText += "  ·  " + owedSaid
    peerTitle = waitingOn
      .map((wait) => [wait.repository, (wait.paths || []).join(", "), wait.releaseCondition].filter(Boolean).join(" · "))
      .join("\n")
  } else if (owedWait) {
    peerText = owedSaid + " · " + (owedWait.releaseCondition || "release")
    peerTitle = waitedOnBy
      .map((wait) => [wait.waiterLabel || wait.waiterSessionId, (wait.paths || []).join(", "), wait.reason].filter(Boolean).join(" · "))
      .join("\n")
  }
  let peerSaid = peerText
    ? `<span class="coordination-wait" title="${L.escapeHTML(peerTitle)}">${L.glyphHTML("⏳", peerText)}</span>`
    : ""

  const work = L.workState(row)
  if (work.state === "waiting_session" && !peerSaid) {
    if (row.work_provenance === "self" && row.work_note && row.work_moved_by && row.work_person_needed === false) {
      const declaredTitle = row.work_moved_by + " · " + row.work_note
      peerSaid = `<span class="coordination-wait" title="${L.escapeHTML(declaredTitle)}">${L.glyphHTML("⏳", L.selfReportedPeerWait(row))}</span>`
    } else {
      const liveRoots = roots.filter(L.taskLive)
      if (liveRoots.length) {
        const childWait = T.webTaskTasks + ": " + liveRoots.map((task) => task.title || task.id).join(" · ")
        peerSaid = `<span class="coordination-wait" title="${L.escapeHTML(childWait)}">${L.glyphHTML("⏳", childWait)}</span>`
      }
    }
  }

  let workSaid = L.workStateHTML(row)
  const closeable = L.closeability(row)
  if (closeable.block) workSaid += L.closeabilityHTML(row)

  const waitShape = [
    ...waitingOn.map((wait) => [wait.id || "wait", wait.ownerLabel || wait.ownerSessionId || "", wait.releaseCondition || ""].join(":")),
    ...waitedOnBy.map((wait) =>
      ["owed", wait.id || "wait", wait.waiterLabel || wait.waiterSessionId || "", wait.releaseCondition || ""].join(":"),
    ),
    ...roots.filter(L.taskLive).map((task) => ["child", task.id || "", task.title || ""].join(":")),
    ...(work.state === "waiting_session" && row.work_provenance === "self"
      ? [["declared", row.work_note || "", row.work_moved_by || "", String(row.work_person_needed)].join(":")]
      : []),
  ].join("+")
  const kind = row.state
  const shape =
    kind +
    "-" +
    row.state +
    "+ws" +
    work.state +
    (closeable.block ? "+cl" + L.closeabilityShape(row) : "") +
    (n ? "+sh" + n : "") +
    (waitShape ? "+cw" + waitShape : "")

  let html: string
  if (work.state === "waiting_you") {
    html = `<span class="wants">${L.glyphHTML("🙋", T.sessionWaiting)}</span>` + peerSaid + workSaid + shellsSaid
  } else if (work.state === "working") {
    html = '<canvas class="spin"></canvas><span class="line"></span>' + peerSaid + workSaid + shellsSaid
  } else if (work.state === "unknown" && row.state === "unknown") {
    html = `<span class="unread">${L.escapeHTML(T.webStateUnreadable)}</span>` + peerSaid + workSaid + shellsSaid
  } else {
    html = peerSaid + workSaid + shellsSaid
  }
  return { html, shape }
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
  const { html, shape } = stateLine(row)
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
  return <div className="state" ref={ref} data-shape={shape} dangerouslySetInnerHTML={{ __html: html }} />
}

/**
 * Where this row sits in somebody's work, as `fillRow` draws it: a session
 * started for another one, or one that did the starting, or a Feature Root.
 * The chip is a claim about this session, true whether or not its root is on
 * screen; the indent is a claim about the row above, so it is drawn only when
 * that row is there.
 */
function taskPlace(row: SessionRow): {
  depth: number
  chip: { text: string; title: string; live: boolean } | null
} {
  const T = L.strings
  const featureRoot = L.featureRootChip(row)
  const task = L.taskOfChild(row.id)
  const kid = task && L.taskShaping(task) ? task : null
  const roots = L.tasksOfRoot(row.id)
  const titles = () => T.webTaskTasks + ": " + roots.map((t) => t.title || t.id).join(" · ")
  if (kid) {
    return {
      depth: L.rowDepth(row.id),
      chip: {
        text: T.webTaskChild + " · " + L.taskWord(kid),
        title: [kid.title || "", roots.length ? titles() : ""].filter(Boolean).join("\n"),
        live: L.taskLive(kid),
      },
    }
  }
  if (featureRoot) return { depth: 0, chip: { text: featureRoot.text, title: featureRoot.title, live: featureRoot.live } }
  if (roots.length) {
    return { depth: 0, chip: { text: T.webTaskRoot + " · " + roots.length, title: titles(), live: roots.some(L.taskLive) } }
  }
  return { depth: 0, chip: null }
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
  const coordinator = L.coordinatorRowModel(row)
  const place = taskPlace(row)
  const waiting = (row.coordination?.waitingOn ?? []).length
  const owed = (row.coordination?.waitedOnBy ?? []).length
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
  const mark = <Mark icon={row.icon} cellPx={4} />
  return (
    <li
      ref={ref}
      className="row"
      role="option"
      tabIndex={-1}
      data-id={row.id}
      data-selection-key={L.selectionKey(row)}
      data-state={row.state}
      data-coordination={waiting ? "waiting" : owed ? "owed" : undefined}
      aria-selected={selected ? "true" : "false"}
      aria-disabled="false"
      data-coordinator={coordinator ? "1" : undefined}
      data-depth={place.chip && place.depth ? String(place.depth) : undefined}
      onClick={() => onOpen(row.id)}
    >
      <span className="kid" hidden={!place.depth} aria-hidden="true">
        └
      </span>
      {coordinator ? (
        // `fillCoordinatorMark`: the canvas moves inside a button, with the
        // crown after it. In the original the button opens the Clawdfather
        // controls; that sheet is not in this console, so a press does nothing
        // rather than open the session behind it.
        <button
          className="coordinator-mark"
          type="button"
          aria-controls="coordinator-controls"
          aria-expanded="false"
          aria-haspopup={coordinator.mark.ariaHaspopup}
          aria-label={coordinator.mark.ariaLabel}
          title={coordinator.mark.ariaLabel}
          onClick={(event) => {
            event.preventDefault()
            event.stopPropagation()
          }}
        >
          {mark}
          <span className="clawdfather-crown" aria-hidden="true" />
        </button>
      ) : (
        mark
      )}
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
        {coordinator && (
          <span className="coordinator-chip" title={coordinator.label}>
            {coordinator.badge}
          </span>
        )}
        {/* The original clears the title to "" rather than removing it, so the
            attribute is there, empty, on a row with no task. */}
        <span
          className="task-chip"
          hidden={!place.chip}
          data-live={place.chip ? (place.chip.live ? "1" : "0") : undefined}
          title={place.chip ? place.chip.title : ""}
        >
          {place.chip ? place.chip.text : ""}
        </span>
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
