import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { sessionMark, sessionName } from "../brand-mark.js"
import { requestConfirm } from "../overlays/events.js"
import { ACTION_WIDTH, actionWidth, archivable, revealFor, swipes, type Reveal } from "./swipe.js"
import { conversationNotStarted } from "./readiness.js"
import { retainedStateWords } from "../session-reading.js"
import { rowPersonaLine } from "../personas.js"
import { usePersonas } from "./PersonaBot.js"
import "./list-density.css"
import "./list-tree.css"
import "./swipe-archive.css"

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

  const notStarted = conversationNotStarted(row)
  const closeable = L.closeability(row)
  const retained = retainedStateWords(row)
  const retainedSaid = retained
    ? `<span class="session-work-copy retained-reading" title="${L.escapeHTML(retained)}">${L.escapeHTML(retained)}</span>`
    : ""
  const attention = row.attention_count
  const attentionSaid = typeof attention === "number" && attention > 0
    ? `<span class="session-attention" aria-label="關注，待處理 ${attention} 筆便條"><span class="session-attention-dot" aria-hidden="true"></span>關注 · 待處理 ${attention}</span>`
    : attention === undefined && row.sessionId
      ? '<span class="session-attention session-attention-unknown">關注未確認</span>'
      : ""
  let workSaid = notStarted ? "" : L.workStateHTML(row)
  // The batch banner owns the source failure. Once an earlier reading is old
  // enough to deserve words, those words ride beside the normal state as the
  // same quiet, single-line annotation used for work and closeability. A
  // conversation that has not started says neither: there is nothing yet to
  // have a state.
  if (!notStarted && closeable.block) workSaid += L.closeabilityHTML(row)

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
    (!notStarted && closeable.block ? "+cl" + L.closeabilityShape(row) : "") +
    (n ? "+sh" + n : "") +
    (waitShape ? "+cw" + waitShape : "") +
    (row.source ? "+src" + row.source.freshness + ":" + row.source.observed_at : "")
    + (attentionSaid ? "+attention" + (attention ?? "unknown") : "")

  let html: string
  if (work.state === "waiting_you") {
    html = `<span class="wants">${L.glyphHTML("🙋", T.sessionWaiting)}</span>` + attentionSaid + peerSaid + workSaid + retainedSaid + shellsSaid
  } else if (work.state === "working") {
    // The spinner alone. The provider's live line ("Wrangling… (5m 1s · …)")
    // is drawn under the conversation itself (`WorkingLine` in Transcript.tsx);
    // in the list it was a second copy of words nobody reads there, and it
    // crowded out the badges beside it.
    html = '<canvas class="spin"></canvas>' + attentionSaid + peerSaid + workSaid + retainedSaid + shellsSaid
  } else if (notStarted) {
    html = `<span class="unread">${L.escapeHTML(nextWord("sessionNotStartedShort"))}</span>` + attentionSaid + peerSaid + workSaid + shellsSaid
  } else if (work.state === "unknown" && row.state === "unknown") {
    // The label already says the state could not be read; the work copy
    // beside it would say so again.
    html = `<span class="unread">${L.escapeHTML(nextWord("sessionStateUnrecognizedList"))}</span>` + attentionSaid + peerSaid + retainedSaid + shellsSaid
  } else {
    html = attentionSaid + peerSaid + workSaid + retainedSaid + shellsSaid
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
/**
 * The row's third line: the role this session was launched as, when there is
 * one, and then `stateLine`'s words. The role is written into the same markup
 * as the words so that it swipes with them and adds no line of its own.
 */
function StateLine({ row, role }: { row: SessionRow; role: ReturnType<typeof rowPersonaLine> }) {
  const ref = useRef<HTMLDivElement>(null)
  const lastCloseability = useRef<SessionRow["closeability"] | null>(null)
  const inventoryOnly = row.closeability?.state === "unknown" &&
    row.closeability.reasons?.some((reason) => reason.code === "session_inventory_stale") &&
    row.closeability.reasons.every((reason) => reason.kind !== "evidence" || reason.code === "session_inventory_stale")
  const prior = lastCloseability.current
  const showPrior = row.source?.freshness === "unverified" && !retainedStateWords(row) && inventoryOnly &&
    (prior?.state === "blocked" || prior?.state === "needs_attestation")
  const { html, shape } = stateLine(showPrior ? { ...row, closeability: prior } : row)
  const roleHTML = role
    ? '<span class="persona-state" title="' + L.escapeHTML(role.title) + '">' +
      '<canvas class="persona-state-bot" width="0" height="0" aria-hidden="true"></canvas>' +
      '<span class="persona-state-name">' + L.escapeHTML(role.name) + "</span></span>"
    : ""
  useLayoutEffect(() => {
    if (row.source?.freshness !== "unverified") lastCloseability.current = row.closeability
  }, [row.closeability, row.source?.freshness])
  useLayoutEffect(() => {
    L.paintSpinner(ref.current?.querySelector<HTMLCanvasElement>("canvas.spin") ?? null)
    if (role) L.paintIcon(ref.current?.querySelector<HTMLCanvasElement>("canvas.persona-state-bot") ?? null, role.persona.icon, 2)
  }, [html, role?.persona])
  return <div className="state" ref={ref} data-shape={shape} dangerouslySetInnerHTML={{ __html: roleHTML + html }} />
}

/**
 * Where this row sits in somebody's work, as `fillRow` draws it: a session
 * started for another one, or one that did the starting, or a Feature Root.
 * The chip is a claim about this session, true whether or not its root is on
 * screen; the indent is a claim about the row above, so it is drawn only when
 * that row is there.
 */
function taskPlace(row: SessionRow, depth: number): {
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
      depth,
      chip: {
        text: T.webTaskChild + " · " + L.taskWord(kid),
        title: [kid.title || "", roots.length ? titles() : ""].filter(Boolean).join("\n"),
        live: L.taskLive(kid),
      },
    }
  }
  if (featureRoot) return { depth, chip: { text: featureRoot.text, title: featureRoot.title + (depth && row.epic_parent ? "\n隸屬於 Epic 負責人的工作樹" : ""), live: featureRoot.live } }
  if (row.epic_parent && depth) return { depth, chip: { text: T.webTaskRoot + " · " + roots.length,
    title: "隸屬於 Epic 負責人的工作樹", live: roots.some(L.taskLive) } }
  if (roots.length) {
    return { depth: 0, chip: { text: T.webTaskRoot + " · " + roots.length, title: titles(), live: roots.some(L.taskLive) } }
  }
  return { depth: 0, chip: null }
}

function agentCount(row: SessionRow): string {
  const provider = (row.agents ?? []).filter((agent) => agent.state === "running").length
  if (row.agents_reading?.state !== "complete") return provider ? `${provider}+?` : "?"
  return provider ? String(provider) : ""
}

/** A known movement as a relative sentence; an unknown reading owns no cell. */
export function sessionActivityWord(
  row: Pick<SessionRow, "activity">,
  now = Date.now(),
  locale = typeof document === "undefined" ? undefined : document.documentElement.lang || undefined,
): string {
  const activity = row.activity
  const at = activity?.at
  if (!activity?.known || typeof at !== "number" || !Number.isFinite(at) || at <= 0) return ""
  // A producer clock a little ahead of this browser still means "now", not
  // that the session's last movement lies in the future.
  const seconds = Math.min(0, at - now / 1000)
  const absolute = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = absolute < 90 * 60 ? "minute" : absolute < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  const value = Math.round(seconds / size)
  let relative: string
  try {
    relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(value, unit)
  } catch {
    // refusal-ok: a browser refusing a locale is not a session-reading refusal.
    relative = new Date(at * 1000).toLocaleString()
  }
  return nextWord("sessionLastActivity", { time: relative })
}

/**
 * One row. The highlight and the open session are two things: arrows move
 * `.selected` without opening anything, and a session can stay open while the
 * highlight is elsewhere. A press opens.
 */
export function Row({
  row,
  depth,
  branchThrough,
  ancestorThrough,
  selected,
  open,
  swiped,
  onOpen,
}: {
  row: SessionRow
  depth: number
  branchThrough: boolean
  ancestorThrough: boolean
  selected: boolean
  open: boolean
  /** This row's action is uncovered (`swipe.ts`). A phone thing; nothing else sets it. */
  swiped: boolean
  onOpen: (id: string) => void
}) {
  const who = L.whoHTML(row.assistant)
  const coordinator = L.coordinatorRowModel(row)
  const place = taskPlace(row, depth)
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
  // The swipe's resting position. While a finger is on the row the gesture
  // writes these itself, frame by frame (`Sessions.tsx`, `paint`) — React is
  // told once, when the row settles, which is why a drag does not re-render
  // thirteen rows sixty times a second.
  useLayoutEffect(() => {
    const node = ref.current
    if (!node) return
    paintSwipe(node, swiped ? "open" : "", swiped ? actionWidthOf(node) : 0)
  }, [swiped, row.sessionId])
  const reveal = swipeReveal(row)
  const name = sessionName(row)
  // 封存 only where there is a conversation to bring back (`swipe.ts`).
  const canArchive = archivable(row.sessionId)
  const activity = sessionActivityWord(row)
  // A gesture that moved the row, and the press that put an uncovered action
  // away, are not presses on the row (`swipe.ts`, `tookThePress`).
  const onPress = () => {
    if (swipes.tookThePress()) return
    onOpen(row.id)
  }
  const icon = sessionMark(row)
  const mark = <Mark icon={icon} cellPx={4} />
  // The role this session was launched as (docs/personas.md), when the
  // machine's catalog names it: its bot and full name at the head of the third
  // line, with name and summary as its title. An id the catalog does not have
  // draws nothing.
  const role = rowPersonaLine(usePersonas(), row.persona)
  return (
    <li
      ref={ref}
      className="row"
      role="option"
      tabIndex={-1}
      data-id={row.id}
      data-selection-key={L.selectionKey(row)}
      data-state={row.state}
      data-attention={row.attention_count && row.attention_count > 0 ? "open" : row.sessionId && row.attention_count === undefined ? "unknown" : undefined}
      data-coordination={waiting ? "waiting" : owed ? "owed" : undefined}
      aria-selected={selected ? "true" : "false"}
      aria-disabled="false"
      data-coordinator={coordinator ? "1" : undefined}
      data-depth={place.chip && place.depth ? String(place.depth) : undefined}
      data-tree-through={branchThrough ? "1" : undefined}
      data-epic-parent={row.epic_parent && depth ? row.epic_parent.epic_id : undefined}
      data-swipe-width={actionWidth(row.sessionId)}
      onClick={onPress}
    >
      <span className="kid" hidden={!place.depth} aria-hidden="true" />
      {ancestorThrough ? <span className="tree-ancestor" aria-hidden="true" /> : null}
      {row.epic_parent && depth ? <span className="sr-only">隸屬於 Epic 負責人的工作樹</span> : null}
      {coordinator ? (
        // The machine steward's identity is visible without promising a
        // command panel that this console does not provide.
        <span className="coordinator-mark" aria-hidden="true">
          {mark}
          <span className="clawdfather-crown" aria-hidden="true" />
        </span>
      ) : (
        mark
      )}
      {coordinator && <span className="coordinator-identity">{coordinator.badge}</span>}
      <div className="title" style={{ color: L.accentTint(icon?.accent) }}>
        <span className="label">{name}</span>
        <span className="who" hidden={!who} dangerouslySetInnerHTML={{ __html: who }} />
      </div>
      <div className="meta">
        <span className="path">{L.path(row.cwd)}</span>
        <span className="tty">{row.tty || row.backend || ""}</span>
        {activity ? <span className="session-activity">{activity}</span> : null}
        <span className="agents-chip" hidden={!agentCount(row)} title={L.strings.webAgents}>
          <span className="dot" />
          <span className="n">{agentCount(row)}</span>
        </span>
        {coordinator && (
          <span className="coordinator-chip" title={coordinator.label} aria-hidden="true">
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
      <StateLine row={row} role={role} />
      {row.machine_scope && !row.coordinator ? (
        <p className="machine-registration" role="status">{nextWord("machineSessionPendingRow")}</p>
      ) : null}
      {/* The phone's swipe control, in `buildRow`'s markup and uncovered by
          `swipe.ts`. It never closes anything: it opens the confirmation every
          other close in this console goes through, which is where the reasons
          are and where the second press is. Hidden until a gesture uncovers it,
          as the original starts it — on a desk nothing ever does. */}
      {/* 封存 beside 關閉 (docs/session-archive.md): the same rule — the press
          opens a confirmation that names the row, never the archive itself.
          Not drawn for a row with no conversation id, which the daemon would
          refuse. */}
      {canArchive ? (
        <button
          className="swipe-archive"
          type="button"
          hidden={!swiped}
          aria-label={nextWord("swipeArchiveLabel", { session: name })}
          title={nextWord("swipeArchiveLabel", { session: name })}
          onClick={(event) => {
            event.stopPropagation()
            requestConfirm({ kind: "archive", id: row.id, opener: event.currentTarget, subject: name, focus: "cancel" })
          }}
        >
          <span className="word">{nextWord("swipeArchiveAction")}</span>
        </button>
      ) : null}
      <button
        className="swipe-end"
        type="button"
        hidden={!swiped}
        data-closeability={reveal.state}
        aria-label={nextWord("swipeEndLabel", { session: name })}
        title={nextWord("swipeEndLabel", { session: name })}
        onClick={(event) => {
          event.stopPropagation()
          requestConfirm({ kind: "end", id: row.id, opener: event.currentTarget, subject: name, focus: "cancel" })
        }}
      >
        <span className="word">{reveal.word}</span>
      </button>
    </li>
  )
}

/**
 * The action the uncovered control offers, with the row's closeability kept
 * only as state for diagnostics and tests.
 *
 * `StateLine` already draws the closeability badge beside the work state. The
 * control therefore says only what pressing it does: open the named close
 * confirmation. That confirmation is where the complete reasons and the
 * second press live. `swipe.ts` holds the rule without importing the UI, so
 * `node --test` can guard it.
 */
function swipeReveal(row: SessionRow): Reveal {
  return revealFor(L.closeabilityOf(row), { end: nextWord("swipeCloseAction") })
}

/**
 * Where a row's contents and its action are, right now.
 *
 * Both are custom properties the copied stylesheet reads
 * (`legacy/responsive.css`): the contents move left by `--swipe-x`, and the
 * button comes in from the edge by `--swipe-button-x`, so what leaves and what
 * arrives are one movement. Written on the element rather than rendered,
 * because a gesture is sixty frames and a render is the whole list.
 */
export function paintSwipe(node: HTMLElement, state: "" | "dragging" | "open", offset: number): void {
  if (state) node.dataset.swipe = state
  else delete node.dataset.swipe
  node.style.setProperty("--swipe-x", -offset + "px")
  node.style.setProperty("--swipe-button-x", Math.max(0, actionWidthOf(node) - offset) + "px")
  for (const action of node.querySelectorAll<HTMLElement>(".swipe-end, .swipe-archive")) action.hidden = !state
}

/** A row's action width as the row wrote it (`data-swipe-width`), else the close's alone. */
export function actionWidthOf(node: HTMLElement): number {
  const width = Number(node.dataset.swipeWidth)
  return Number.isFinite(width) && width > 0 ? width : ACTION_WIDTH
}
