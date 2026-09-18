import { useEffect, useRef, useState, useSyncExternalStore, type RefObject } from "react"
import type { ScheduleRow, SessionRow, TaskRow } from "@clawdline/contract"
import { client } from "./client.js"
import * as L from "./legacy/bridge.js"
import { Row } from "./session/List.js"
import { Detail } from "./session/Detail.js"
import { Start, StartSheet, StartingRow } from "./session/Start.js"
import { Starting } from "./session/Starting.js"
import { pushShape, startPush, subscribePush, togglePush } from "./push/push.js"

/**
 * The session list page: the list, and the conversation beside it.
 *
 * The parts live in ./session/ so they can be worked on without three people
 * editing one file. The markup in each is the original's — same elements, same
 * class names — because the stylesheet beside them is the original's, copied.
 */
export function SessionsPage({
  rows,
  loaded,
  arrived,
  live,
  emptyAuthoritative,
  shown: onScreen,
  view,
  paneOpen,
  filter,
  onFilter,
  selected,
  openId,
  onOpen,
  onBack,
  onDid,
}: {
  rows: SessionRow[]
  /** The first reading has been answered, with a list or with a failure. */
  loaded: boolean
  /** A list has arrived: the original's `S.arrived`. */
  arrived: boolean
  /** The stream is up: the original's `S.conn === "live"`. */
  live: boolean
  emptyAuthoritative: boolean
  /** This is the page on screen. Another page hides it; nothing takes it down. */
  shown: boolean
  /** The phone's one screen at a time: `main#app[data-view]`. */
  view: "list" | "detail"
  /** The desk's second column, on or off (⌘J): `main#app[data-pane]`. */
  paneOpen: boolean
  filter: string
  onFilter: (q: string) => void
  /** The highlight in the list (`li.selected`). */
  selected: string | null
  /** The session in the detail pane (`li.open`). */
  openId: string | null
  onOpen: (id: string) => void
  onBack: () => void
  onDid: () => void
}) {
  // The task list the chips, the indent and the detail header read. Fetched
  // here rather than taken from the stream because the page's stream reader
  // does not hand its frames to this page.
  const tasks = useTasks(arrived, rows)

  // The copied modules read a module-level object, so it is filled before
  // anything is drawn from them, and the list's own order and filter are used.
  L.publish(rows, openId, filter, selected, tasks)
  // A session started from this page sits at the top while it arrives, and
  // the place it is arriving for stands there until it does (`Start.arrange`,
  // `Start.placeholder`). Both change with the wait, which is not a prop.
  useSyncExternalStore(Start.subscribe, Start.version)
  const shown = Start.arrange(L.orderedRows())
  const arriving = Start.placeholder()
  // From the whole fleet, as `byId` looks it up: a filter that hides the open
  // row does not close it.
  const open = rows.find((r) => r.id === openId) ?? null
  const T = L.strings

  // `thawOrder` redraws the list through the session UI seam once the order it
  // held is let go, and this page is that list.
  const [, redraw] = useState(0)
  useEffect(() => {
    L.bindSessionUI({ renderList: () => redraw((n) => n + 1) })
  }, [])

  // The start sheet opens a row the way the list does, and asks which is open.
  const openIdRef = useRef(openId)
  openIdRef.current = openId
  const onOpenRef = useRef(onOpen)
  onOpenRef.current = onOpen
  const onDidRef = useRef(onDid)
  onDidRef.current = onDid
  useEffect(() => {
    Start.host({
      open: (id) => onOpenRef.current(id),
      openId: () => openIdRef.current,
      refresh: () => onDidRef.current(),
    })
  }, [])
  // Every list that arrives, until the one with the started session in it
  // (`Start.check`, which `renderList` calls in the original).
  useEffect(() => {
    Start.check()
  }, [rows])

  // `Waits.list` and `listUnknown()` (`view/waits.js`). Nothing is drawn for
  // the first 150ms; after that a skeleton stands in for the rows, and once up
  // it stays 320ms. The first answer settles it — a failure is an answer too,
  // and the stream opens only after one — and until then neither the list nor
  // the detail head has anything true to say.
  const wait = useWait(loaded)
  const skeleton = wait === "shown"
  const listUnknown = !arrived && wait !== "over"
  const drawn = skeleton ? [] : shown

  // Every spinner in the list, handed to the one clock that drives them all.
  // Re-registered after each render because the rows are rebuilt: a canvas that
  // has left the document must leave the list with it.
  const listRef = useRef<HTMLUListElement>(null)
  useEffect(() => {
    L.registerSpinners([...(listRef.current?.querySelectorAll<HTMLCanvasElement>("canvas.spin") ?? [])])
  })

  // The empty state, `renderList`'s tail. Four cases told apart: nothing
  // matches what was typed, there are genuinely no sessions, or nothing has
  // arrived yet (a stream that is not up, or a reading that did not complete).
  // The original's fifth, a browser that was refused, has no counterpart on
  // this daemon. What the element holds is only rewritten when it is shown, as
  // there, so a skeleton that has been taken down is still inside it, hidden.
  const empty = !skeleton && drawn.length === 0 && !arriving && !listUnknown
  const homeEmpty = empty && rows.length === 0 && !filter
  const said = useRef<"skel" | [string, string] | null>(null)
  if (skeleton) said.current = "skel"
  else if (empty) {
    said.current = rows.length
      ? [L.fillString(T.webEmptyFilterTitle, { q: filter }), T.webEmptyFilterHint]
      : live && emptyAuthoritative
        ? [T.noSession, T.webEmptyNoneHint]
        : [T.webEmptyWaitTitle, T.webEmptyWaitHint]
  }
  const emptyClass = skeleton ? "skel" : "empty" + (homeEmpty ? " home-hero-list" : "")

  const scrollRef = useRef<HTMLDivElement>(null)
  const ptrRef = useRef<HTMLDivElement>(null)
  const ptrWord = usePullToRefresh(scrollRef, ptrRef, onDid)
  useOrderHold(scrollRef)
  const schedules = useSchedules(arrived)

  return (
    <>
      {/* Before the page, as in `index.html`: the band is news about the Mac,
          and stays in view on whatever page is showing. */}
      <Starting />
      <main
        className="app"
        id="app"
        data-page-view="sessions"
        data-view={view}
        data-pane={paneOpen ? "on" : "off"}
        hidden={!onScreen}
      >
        <section className="pane pane-list">
          <div className="filter-row">
            {/* Every attribute here keeps a password manager out of a box that
                filters a list (`index.html`). Uncontrolled, because a controlled
                input writes a `value` attribute the original does not have. */}
            <input
              id="filter"
              type="search"
              name="q7f3"
              placeholder={T.webFilterPlaceholder}
              autoComplete="off"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              data-1p-ignore=""
              data-lpignore="true"
              data-bwignore=""
              data-form-type="other"
              aria-label={T.webFilterLabel}
              onChange={(e) => onFilter(e.target.value)}
            />
            <span className="slash">/</span>
            {/* Saying what to start leads to the command sheet, with its
                dictation and draft, which this page does not have, so it is
                disabled. Picking where to start opens the start sheet. */}
            <button
              className="start"
              id="voice-go"
              type="button"
              title={T.webCommand}
              aria-label={T.webCommandLabel}
              aria-pressed="false"
              disabled
            >
              <svg className="ico ico-mic" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor"></rect>
                <path
                  d="M5.75 11.75v0.5a6.25 6.25 0 0 0 12.5 0v-0.5M12 18.5V21"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.8"
                  strokeLinecap="round"
                ></path>
              </svg>
              <svg className="ico ico-stop" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <rect x="7" y="7" width="10" height="10" rx="2.5" fill="currentColor"></rect>
              </svg>
            </button>
            <button
              className="start"
              id="start-go"
              type="button"
              title={T.webStart}
              aria-label={T.webStartLabel}
              onClick={() => Start.open()}
            >
              <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
                <path d="M7 2.6v8.8M2.6 7h8.8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round"></path>
              </svg>
            </button>
          </div>
          <div className="scroller list-scroll" id="list-scroll" ref={scrollRef}>
            <div className="ptr" id="ptr" ref={ptrRef}>
              <span id="ptr-label">{ptrWord === "release" ? T.webPullRelease : ptrWord === "busy" ? T.webPullBusy : T.webPull}</span>
            </div>
            <ul className="rows" id="rows" role="listbox" aria-label={T.webListLabel} tabIndex={0} ref={listRef}>
              {!skeleton && arriving && <StartingRow place={arriving} />}
              {drawn.map((r) => (
                <Row key={r.id} row={r} selected={r.id === selected} open={r.id === openId} onOpen={onOpen} />
              ))}
            </ul>
            <div className={emptyClass} id="list-empty" hidden={!skeleton && !empty}>
              {said.current === "skel" ? (
                <ListSkeleton />
              ) : said.current ? (
                <>
                  <b>{said.current[0]}</b>
                  {said.current[1]}
                </>
              ) : null}
            </div>
            <Schedules list={schedules} />
          </div>
          <NotifyFooter />
        </section>

        <Detail row={open} onBack={onBack} onDid={onDid} listUnknown={listUnknown} />
      </main>
      <StartSheet />
    </>
  )
}

/**
 * `Push.draw`'s footer half (`input/push.js`): whether this device is told when
 * a session waits.
 *
 * **Only the two states somebody can act on from here keep a place in the
 * flow.** Off is an offer and needs a button; on iOS in a tab there is no
 * button to have, and the one sentence that gets somebody to a working
 * notification is the whole feature until they have read it. Everything else —
 * already on, blocked, this browser cannot — is a fact rather than a thing to
 * do, and a fact does not get a permanent row of the screen.
 *
 * `settled` first: a footer that has not been decided yet is not in the flow,
 * whatever the placeholder state says. Appearing a few frames late is a layout
 * shift; appearing and then vanishing is a fault.
 */
function NotifyFooter() {
  const T = L.strings
  const push = useSyncExternalStore(subscribePush, pushShape)
  useEffect(startPush, [])
  const inFlow = push.settled && (push.state === "off" || push.state === "homescreen")
  return (
    <div className="notify" id="notify" hidden={!inFlow} data-state={push.state}>
      <button
        className="go"
        id="notify-go"
        type="button"
        hidden={push.state !== "off"}
        disabled={push.busy}
        onClick={togglePush}
      >
        <svg className="bell" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
          <circle cx="8" cy="1.7" r="1" fill="currentColor"></circle>
          <path
            fill="currentColor"
            d="M8 2.2a3.9 3.9 0 0 1 3.9 3.9v2.6l1.05 1.75H3.05L4.1 8.7V6.1A3.9 3.9 0 0 1 8 2.2Z"
          ></path>
          <path fill="currentColor" d="M6.3 11.6h3.4a1.7 1.7 0 0 1-3.4 0Z"></path>
        </svg>
        <span id="notify-go-label">{push.busy ? T.webNotifyAsking : T.webNotifyGo}</span>
      </button>
      <span className="say" id="notify-say">
        {push.state === "homescreen" ? T.webNotifyHomeScreen : T.webNotifyOff}
      </span>
    </div>
  )
}

/**
 * `Waiting` (`view/waits.js`) for the one wait this page has: armed at once,
 * shown after `showAfter`, and once shown kept for `minShown` after it settles.
 */
function useWait(settled: boolean, showAfter = 150, minShown = 320): "armed" | "shown" | "over" {
  const [phase, setPhase] = useState<"armed" | "shown" | "over">(settled ? "over" : "armed")
  const shownAt = useRef(0)
  useEffect(() => {
    if (phase !== "armed" || settled) return
    const timer = setTimeout(() => {
      shownAt.current = Date.now()
      setPhase("shown")
    }, showAfter)
    return () => clearTimeout(timer)
  }, [phase, settled, showAfter])
  useEffect(() => {
    if (!settled || phase === "over") return
    const left = phase === "armed" ? 0 : minShown - (Date.now() - shownAt.current)
    if (left <= 0) {
      setPhase("over")
      return
    }
    const timer = setTimeout(() => setPhase("over"), left)
    return () => clearTimeout(timer)
  }, [phase, settled, minShown])
  return phase
}

/** `drawListSkeleton`: the rows about to arrive, at fixed widths so it never reshuffles. */
function ListSkeleton() {
  const widths = [
    [64, 41],
    [78, 52],
    [49, 37],
    [71, 45],
  ]
  return (
    <div role="status" aria-label={L.strings.webLoading}>
      {widths.map(([line, sub], i) => (
        <div className="skel-row" key={i}>
          <span className="bar mark"></span>
          <span className="bar line" style={{ width: `${line}%` }}></span>
          <span className="bar sub" style={{ width: `${sub}%` }}></span>
        </div>
      ))}
    </div>
  )
}

/**
 * The order is held still while the pointer is inside the list, and for a
 * moment after a finger leaves it (`input/keys.js`): a list that sorts itself
 * can move a row out from under a press.
 */
function useOrderHold(scrollRef: RefObject<HTMLDivElement | null>): void {
  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller) return
    const touchEnd = () => {
      setTimeout(L.thawOrder, 1200)
    }
    scroller.addEventListener("mouseenter", L.freezeOrder)
    scroller.addEventListener("mouseleave", L.thawOrder)
    scroller.addEventListener("touchstart", L.freezeOrder, { passive: true })
    scroller.addEventListener("touchend", touchEnd, { passive: true })
    return () => {
      scroller.removeEventListener("mouseenter", L.freezeOrder)
      scroller.removeEventListener("mouseleave", L.thawOrder)
      scroller.removeEventListener("touchstart", L.freezeOrder)
      scroller.removeEventListener("touchend", touchEnd)
    }
  }, [scrollRef])
}

/**
 * Pull to refresh, phones only (`input/edges.js`): with resistance, released
 * past 62px it reads again and holds a 34px pad until the answer is in. The
 * original reloads the page instead when it is stale; this page has no stale
 * notice to answer.
 */
function usePullToRefresh(
  scrollRef: RefObject<HTMLDivElement | null>,
  padRef: RefObject<HTMLDivElement | null>,
  refresh: () => void,
): "pull" | "release" | "busy" {
  const [word, setWord] = useState<"pull" | "release" | "busy">("pull")
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh
  useEffect(() => {
    const scroller = scrollRef.current
    const pad = padRef.current
    if (!scroller || !pad) return
    const THRESHOLD = 62
    let startY = 0
    let pulling = false
    let distance = 0
    let busy = false
    let alive = true
    const start = (ev: TouchEvent) => {
      if (busy || scroller.scrollTop > 0 || ev.touches.length !== 1) return
      startY = ev.touches[0].clientY
      pulling = true
      distance = 0
      pad.classList.add("dragging")
    }
    const move = (ev: TouchEvent) => {
      if (!pulling) return
      const raw = ev.touches[0].clientY - startY
      if (raw <= 0) {
        distance = 0
        pad.style.height = "0px"
        return
      }
      distance = Math.min(90, Math.pow(raw, 0.82))
      pad.style.height = distance + "px"
      setWord(distance >= THRESHOLD ? "release" : "pull")
    }
    const end = () => {
      if (!pulling) return
      pulling = false
      pad.classList.remove("dragging")
      if (distance >= THRESHOLD && !busy) {
        busy = true
        setWord("busy")
        pad.style.height = "34px"
        void Promise.resolve(refreshRef.current()).then(() => {
          setTimeout(() => {
            if (!alive) return
            pad.style.height = "0px"
            setWord("pull")
            busy = false
          }, 260)
        })
      } else {
        pad.style.height = "0px"
      }
    }
    scroller.addEventListener("touchstart", start, { passive: true })
    scroller.addEventListener("touchmove", move, { passive: true })
    scroller.addEventListener("touchend", end, { passive: true })
    scroller.addEventListener("touchcancel", end, { passive: true })
    return () => {
      alive = false
      scroller.removeEventListener("touchstart", start)
      scroller.removeEventListener("touchmove", move)
      scroller.removeEventListener("touchend", end)
      scroller.removeEventListener("touchcancel", end)
    }
  }, [scrollRef, padRef])
  return word
}

/**
 * The schedule inventory, read as `net/schedules.js` reads it: not before a
 * list has arrived, then once a minute while the page is visible, and at once
 * on return if a minute was missed. A failed read draws nothing — the section
 * stays absent before any answer and keeps the last truthful list after one.
 */
/**
 * The dispatched-work list, `S.tasks` in the original.
 *
 * The original is handed the whole list on its stream's `orchestrator` frame,
 * every time a task moves. This page's stream reader (`useFleet`, outside this
 * file) passes on only session frames, so the list is read here instead: once
 * when the first session list arrives, again whenever the rows change shape —
 * a tab opens or closes, a session starts or stops working, which is when a
 * task is briefed or finishes — at most every 1.5 seconds, and on a 10-second
 * lane for the moves that change no row. A failed read keeps the last list:
 * a chip that vanished because one request failed would be a false statement
 * that the task is over.
 */
function useTasks(arrived: boolean, rows: SessionRow[]): TaskRow[] | null {
  const [list, setList] = useState<TaskRow[] | null>(null)
  const readRef = useRef<() => void>(() => {})
  useEffect(() => {
    if (!arrived) return
    const LANE_MS = 10000
    const GAP_MS = 1500
    let alive = true
    let inFlight = false
    let again = false
    let last = 0
    let timer: ReturnType<typeof setTimeout> | null = null
    const read = () => {
      if (inFlight) {
        again = true
        return
      }
      const wait = last + GAP_MS - Date.now()
      if (wait > 0) {
        if (timer === null) {
          timer = setTimeout(() => {
            timer = null
            read()
          }, wait)
        }
        return
      }
      inFlight = true
      last = Date.now()
      client
        .tasks()
        .then((d) => {
          if (alive) setList(d.tasks ?? [])
        })
        .catch(() => {})
        .finally(() => {
          inFlight = false
          if (alive && again) {
            again = false
            read()
          }
        })
    }
    readRef.current = read
    read()
    let lane: ReturnType<typeof setInterval> | null = document.hidden ? null : setInterval(read, LANE_MS)
    const onVisibility = () => {
      if (document.hidden) {
        if (lane !== null) clearInterval(lane)
        lane = null
        return
      }
      if (lane !== null) return
      lane = setInterval(read, LANE_MS)
      if (Date.now() - last >= LANE_MS) read()
    }
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      alive = false
      readRef.current = () => {}
      if (lane !== null) clearInterval(lane)
      if (timer !== null) clearTimeout(timer)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [arrived])
  // The rows' shape: which sessions exist and what they are doing. A working
  // line that ticks every second is not a change of shape.
  const shape = rows.map((r) => `${r.id}:${r.state}:${r.work_state}`).join("|")
  useEffect(() => {
    readRef.current()
  }, [shape])
  return list
}

function useSchedules(arrived: boolean): ScheduleRow[] | null {
  const [list, setList] = useState<ScheduleRow[] | null>(null)
  useEffect(() => {
    if (!arrived) return
    const LANE_MS = 60000
    let alive = true
    let inFlight = false
    let last = 0
    const read = () => {
      if (inFlight) return
      inFlight = true
      last = Date.now()
      client
        .schedules()
        .then((d) => {
          if (alive) setList(d.schedules ?? [])
        })
        .catch(() => {})
        .finally(() => {
          inFlight = false
        })
    }
    read()
    let lane: ReturnType<typeof setInterval> | null = document.hidden ? null : setInterval(read, LANE_MS)
    const onVisibility = () => {
      if (document.hidden) {
        if (lane !== null) clearInterval(lane)
        lane = null
        return
      }
      if (lane !== null) return
      lane = setInterval(read, LANE_MS)
      if (Date.now() - last >= LANE_MS) read()
    }
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      alive = false
      if (lane !== null) clearInterval(lane)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [arrived])
  return list
}

/**
 * `details#schedules`, `renderSchedules` (`view/schedules.js`).
 *
 * Hidden until an answer lands, and then hidden only when it is empty and this
 * browser may not write. This daemon's `/v1/health` carries no `write` flag and
 * the composer treats the page as writable until a send is refused, so the
 * section is shown for an empty answer as well.
 *
 * The `+` and each row lead to the schedule form and the run-history sheet,
 * which this page does not have, so the `+` is disabled and a row does nothing
 * when pressed. The rows keep the original's `role="button"` shape.
 */
function Schedules({ list }: { list: ScheduleRow[] | null }) {
  const T = L.strings
  const schedules = list ?? []
  // "Schedules" and the list's name are English in the original's markup and
  // nothing paints them, so they are English here.
  return (
    <details className="schedules" id="schedules" open hidden={list === null}>
      <summary
        onClick={(ev) => {
          // The button sits inside the summary; its press must not fold the list.
          if ((ev.target as Element).closest("#schedule-new")) ev.preventDefault()
        }}
      >
        <span>Schedules</span>
        <span className="count" id="schedules-count">
          {schedules.length ? String(schedules.length) : ""}
        </span>
        <button
          className="add"
          id="schedule-new"
          type="button"
          title={T.webScheduleNew}
          aria-label={T.webScheduleNew}
          disabled
        >
          <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
            <path d="M7 2.6v8.8M2.6 7h8.8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round"></path>
          </svg>
        </button>
      </summary>
      <ul className="schedule-rows" id="schedule-rows" aria-label="Scheduled tasks">
        {schedules.map((s) => (s.unreadable ? <InvalidSchedule key={s.id} row={s} /> : <ScheduleItem key={s.id} row={s} />))}
      </ul>
    </details>
  )
}

/**
 * `validRow`. What this wire does not carry is left out rather than guessed:
 * there is no last-run outcome (only its task id), so the result is the
 * original's "—"; no next firing time, so the next-run line is empty rather
 * than claiming there is none; no missed-run time; and no project record, so
 * the project is the schedule's own directory, named by its last component as
 * `scheduleRunsHTML` names a run's, with no mark to draw.
 */
function ScheduleItem({ row }: { row: ScheduleRow }) {
  const T = L.strings
  const parts = row.dir.split("/").filter(Boolean)
  const label = row.dir === "/" ? "/" : (parts.pop() ?? "")
  const project = row.dir ? { label, path: row.dir } : null
  // `nextLine` in the original; empty because nothing here says when it fires next.
  const nextLine: string = ""
  return (
    <li className="schedule-row" data-id={row.id} role="button" tabIndex={0} style={{ cursor: "pointer" }}>
      <div className="schedule-name">
        <span
          className="enabled-dot"
          data-enabled={row.enabled ? "1" : "0"}
          role="img"
          aria-label={row.enabled ? T.webScheduleEnabled : T.webScheduleDisabled}
        ></span>
        <span className="schedule-title">{row.name || "Untitled schedule"}</span>
      </div>
      <span className="schedule-result" data-state="none">
        —
      </span>
      <div className="schedule-meta">
        {project && (
          <span className="schedule-project">
            <ScheduleMark />
            <span className="schedule-project-name" title={project.path}>
              {project.label}
            </span>
          </span>
        )}
        {project && nextLine && (
          <span className="schedule-meta-sep" aria-hidden="true">
            {" · "}
          </span>
        )}
        <time className="schedule-next">{nextLine}</time>
      </div>
    </li>
  )
}

/** The project mark, drawn by the rows' code; with no icon it draws nothing and is marked so. */
function ScheduleMark() {
  const ref = useRef<HTMLCanvasElement>(null)
  const [none, setNone] = useState(false)
  useEffect(() => {
    setNone(!L.paintIcon(ref.current, undefined, 3))
  }, [])
  return <canvas className={none ? "schedule-project-mark none" : "schedule-project-mark"} aria-hidden="true" ref={ref} />
}

/**
 * `invalidRow`. The original names the file and the parse error; this wire has
 * neither, so the row is named by the schedule and the error is the
 * original's own fallback sentence (English in its source, as here).
 */
function InvalidSchedule({ row }: { row: ScheduleRow }) {
  return (
    <li className="schedule-row invalid">
      <div className="schedule-name">
        <span className="enabled-dot" data-enabled="invalid" role="img" aria-label="Invalid"></span>
        <span className="schedule-title">{row.name || "Invalid schedule"}</span>
      </div>
      <span className="schedule-result" data-state="invalid">
        invalid
      </span>
      <p className="schedule-error">The schedule could not be read.</p>
    </li>
  )
}
