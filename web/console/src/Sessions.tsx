import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type RefObject } from "react"
import type { BearingsSource, RestorableSession, ScanSource, SessionRow, TaskRow } from "@clawdline/contract"
import { client } from "./client.js"
import * as L from "./legacy/bridge.js"
import { paintSwipe, Row } from "./session/List.js"
import { Detail } from "./session/Detail.js"
import { Start, StartSheet, StartingRow } from "./session/Start.js"
import { Command, CommandSheet } from "./session/Command.js"
import { Starting } from "./session/Starting.js"
import { RestoreCard, RestoreHero, RestoreSheet, useRestoreOffer } from "./session/Restore.js"
import { offerShape } from "./session/restore-offer.js"
import { taskReads } from "./session/task-read.js"
import { swipes } from "./session/swipe.js"
import { pushShape, startPush, subscribePush, togglePush } from "./push/push.js"
import { ScheduleSection } from "./pages/schedules.js"
import { nextWord } from "./next-strings.js"
import { batchReadingWords, scanFailureWords } from "./session-reading.js"
import { openNewWorkItem } from "./pages/work/new-item.js"

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
  readingSource,
  scanNotes,
  scanSources,
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
  /** The whole batch's freshness; rows carry the source they came from. */
  readingSource?: BearingsSource
  /** Why a terminal source did not finish, plus its named unread regions. */
  scanNotes?: string[]
  scanSources?: ScanSource[]
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
  const sourceFailure = scanFailureWords(scanNotes, scanSources)
  const readingSaid = sourceFailure ?? batchReadingWords(readingSource)

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
    Command.host({
      open: (id) => onOpenRef.current(id),
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
  useListReorderAnimation(listRef)

  // The empty state, `renderList`'s tail. Four cases told apart: nothing
  // matches what was typed, there are genuinely no sessions, nothing has
  // arrived because there is no line, and — the fourth — a line that is up
  // over a list that has not been stated.
  //
  // **The last two are not the same thing and no longer say the same words.**
  // "Waiting for the app" is true of a page with nothing to talk to. It was
  // also what a page saw with its stream up, its machine in the list and its
  // schedules on screen, waiting on one envelope that machine had no reason to
  // re-send (`internal/transport/cloud/publish.go`'s unchanged-row skip): the
  // sentence sent the person to look at an app that was running and connected.
  // A different fact gets a different sentence, and this one names what is
  // actually missing.
  //
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
      : !live
        ? [T.webEmptyWaitTitle, T.webEmptyWaitHint]
        : emptyAuthoritative
          ? [T.noSession, T.webEmptyNoneHint]
          : sourceFailure
            ? [nextWord("sessionsListIncompleteTitle"), sourceFailure]
            : [nextWord("sessionsListWaitTitle"), nextWord("sessionsListWaitHint")]
  }
  const emptyClass = skeleton ? "skel" : "empty" + (homeEmpty ? " home-hero-list" : "")

  // The sessions a reboot took away (docs/session-restore.md). Offered in the
  // home hero's place when the list is empty, and as a card above the rows
  // when the person has already opened something; nothing new is drawn when
  // there is nothing to offer, so the empty words above stay what they were.
  const restore = useRestoreOffer(live)
  const [restoring, setRestoring] = useState<RestorableSession[] | null>(null)
  const offerAt = offerShape(restore.offer, skeleton || listUnknown ? "loading" : homeEmpty ? "home" : "rows")
  const openRestore = () => setRestoring(restore.offer ? [...restore.offer.sessions] : null)

  const scrollRef = useRef<HTMLDivElement>(null)
  const ptrRef = useRef<HTMLDivElement>(null)
  // The swipe binds first, so that by the time pull-to-refresh reads a move
  // the axis has been decided and it knows whether the gesture is its own.
  const swipedId = useSwipeToEnd(scrollRef)
  const ptrWord = usePullToRefresh(scrollRef, ptrRef, onDid)
  useOrderHold(scrollRef)
  // A row that has gone takes its uncovered action with it, rather than
  // leaving a close button standing over whatever row took its place.
  useEffect(() => {
    if (swipedId && !drawn.some((r) => r.id === swipedId)) swipes.closeOpen()
  }, [drawn, swipedId])

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
            {/* Saying what to start opens the voice-to-draft command sheet;
                picking where to start opens the ordinary start sheet. */}
            <button
              className="start"
              id="voice-go"
              type="button"
              title={T.webCommand}
              aria-label={T.webCommandLabel}
              aria-pressed="false"
              onClick={() => Command.openAndListen()}
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
              id="work-create-go"
              type="button"
              title="新增看板項目"
              aria-label="新增看板項目"
              onClick={() => openNewWorkItem()}
            >
              <svg className="ico ico-work-add" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <rect x="4" y="5" width="11" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth="1.7"></rect>
                <path d="M7.5 9h4M7.5 12.5h4M18.5 10.5v7M15 14h7" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round"></path>
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
          <div className="session-reading" role="status" hidden={!readingSaid}>
            {readingSaid}
          </div>
          <div className="scroller list-scroll" id="list-scroll" ref={scrollRef}>
            <div className="ptr" id="ptr" ref={ptrRef}>
              <span id="ptr-label">{ptrWord === "release" ? T.webPullRelease : ptrWord === "busy" ? T.webPullBusy : T.webPull}</span>
            </div>
            {offerAt === "compact" && restore.offer && <RestoreCard offer={restore.offer} onOpen={openRestore} />}
            <ul className="rows" id="rows" role="listbox" aria-label={T.webListLabel} tabIndex={0} ref={listRef}>
              {!skeleton && arriving && <StartingRow place={arriving} />}
              {drawn.map((r) => (
                <Row
                  key={L.selectionKey(r)}
                  row={r}
                  selected={r.id === selected}
                  open={r.id === openId}
                  swiped={r.id === swipedId}
                  onOpen={onOpen}
                />
              ))}
            </ul>
            <div className={emptyClass} id="list-empty" hidden={!skeleton && !empty}>
              {said.current === "skel" ? (
                <ListSkeleton />
              ) : offerAt === "hero" && restore.offer ? (
                <RestoreHero offer={restore.offer} onOpen={openRestore} />
              ) : said.current ? (
                <>
                  <b>{said.current[0]}</b>
                  {said.current[1]}
                </>
              ) : null}
            </div>
            <ScheduleSection arrived={arrived} onOpen={onOpen} />
          </div>
          <NotifyFooter />
        </section>

        <Detail row={open} tasks={tasks} onOpenSession={onOpen} onBack={onBack} onDid={onDid} listUnknown={listUnknown} />
      </main>
      <StartSheet />
      <CommandSheet />
      {restoring && (
        <RestoreSheet sessions={restoring} onClose={() => setRestoring(null)} onChanged={restore.refresh} />
      )}
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
      // And not while a row's action is uncovered: the swipe is still on
      // screen, so the row under it must stay where the finger left it.
      setTimeout(() => {
        if (swipes.openId() === null) L.thawOrder()
      }, 1200)
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
 * The original list keeps each row node and uses FLIP when its sorted position
 * changes (`Resources/web/app/js/view/list.js`). React already keeps the keyed
 * nodes; this is the half the port lost: remember their layout positions after
 * each commit, then start the next order at those positions and let it travel
 * to the new ones.
 *
 * `offsetTop` is the untransformed layout answer. Reading a transformed client
 * rect here would feed a still-running animation back into the next render and
 * make a live-line update restart or shorten the movement. An unchanged order
 * records its current layout but leaves the animation alone.
 */
function useListReorderAnimation(listRef: RefObject<HTMLUListElement | null>): void {
  const previous = useRef<{ order: string[]; tops: Map<string, number> } | null>(null)
  const animations = useRef(new Map<string, Animation>())

  useLayoutEffect(() => {
    const list = listRef.current
    if (!list) return
    const nodes = [...list.querySelectorAll<HTMLElement>(":scope > li.row[data-selection-key]")]
    const order = nodes.map((node) => node.dataset.selectionKey ?? "")
    const tops = new Map(nodes.map((node) => [node.dataset.selectionKey ?? "", node.offsetTop]))
    const before = previous.current
    previous.current = { order, tops }

    const sameOrder =
      before?.order.length === order.length && before.order.every((key, index) => key === order[index])
    if (!before || sameOrder || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return

    for (const node of nodes) {
      const key = node.dataset.selectionKey ?? ""
      const from = before.tops.get(key)
      if (from === undefined) continue
      const delta = from - node.offsetTop
      if (Math.abs(delta) < 1) continue

      animations.current.get(key)?.cancel()
      const animation = node.animate(
        [{ transform: `translateY(${delta}px)` }, { transform: "none" }],
        { duration: 240, easing: "cubic-bezier(.2, .7, .2, 1)" },
      )
      animations.current.set(key, animation)
      animation.onfinish = animation.oncancel = () => {
        if (animations.current.get(key) === animation) animations.current.delete(key)
      }
    }
  })

  useEffect(
    () => () => {
      for (const animation of animations.current.values()) animation.cancel()
      animations.current.clear()
    },
    [],
  )
}

/**
 * Swiping a row left, phones only: the gesture, bound to the list.
 *
 * The rule is `session/swipe.ts`, which imports nothing and is held by
 * `node --test`; this is the part that has to be in a browser. What it does
 * between a finger going down and coming up is write two custom properties on
 * one `li` — the contents leave by `--swipe-x`, the action arrives by
 * `--swipe-button-x`, both read by the copied `legacy/responsive.css`. React
 * is told once, when the row settles, because a render is the whole list and a
 * drag is sixty frames of one row.
 *
 * **Every listener is passive and none of them calls `preventDefault`**, which
 * a passive listener may not do anyway. Nothing needs it: `.row[data-swipe]`
 * carries `touch-action: pan-y`, so the browser gives the page every
 * horizontal movement over a row and keeps only the vertical ones for itself.
 * The attribute is therefore put on the row the moment a finger lands on it —
 * `arm`, before any movement has been read — and taken off again when the
 * gesture turns out to be the scroller's. Set after the first move instead, the
 * browser has already begun a horizontal scroll and the row jumps.
 *
 * The scroller is where the listeners go, not each row: rows are rebuilt on
 * every frame the daemon sends, and a listener per row would be rebound with
 * them, mid-gesture.
 */
function useSwipeToEnd(scrollRef: RefObject<HTMLDivElement | null>): string | null {
  const swiped = useSyncExternalStore(swipes.subscribe, swipes.openId, swipes.openId)
  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller) return
    let node: HTMLElement | null = null
    const rowAt = (target: EventTarget | null): HTMLElement | null => {
      const el = target instanceof Element ? target.closest<HTMLElement>("li.row") : null
      return el && scroller.contains(el) ? el : null
    }
    /** The row is ready to be dragged before it is dragged; see above. */
    const arm = (el: HTMLElement) => {
      if (!el.dataset.swipe) el.dataset.swipe = "dragging"
    }
    const paint = (el: HTMLElement, id: string) => {
      paintSwipe(el, swipes.stateOf(id), swipes.offsetOf(id))
    }
    const start = (ev: TouchEvent) => {
      if (ev.touches.length !== 1) return
      const el = rowAt(ev.target)
      const id = el?.dataset.id ?? null
      const closed = swipes.begin(id, ev.touches[0].clientX, ev.touches[0].clientY, ev.timeStamp)
      if (closed) {
        for (const other of scroller.querySelectorAll<HTMLElement>("li.row[data-swipe]")) paintSwipe(other, "", 0)
        node = null
        return
      }
      node = el
      if (el) arm(el)
    }
    const move = (ev: TouchEvent) => {
      if (!node || ev.touches.length !== 1) return
      const axis = swipes.move(ev.touches[0].clientX, ev.touches[0].clientY, ev.timeStamp)
      if (axis === "list") {
        // The scroller's after all: put the row back exactly as it was, so a
        // scroll that started over a row leaves no trace of having been armed.
        paintSwipe(node, swipes.stateOf(node.dataset.id ?? ""), swipes.offsetOf(node.dataset.id ?? ""))
        node = null
        return
      }
      if (axis !== "row") return
      // The order is frozen by the list's own `touchstart` listener; this only
      // has to not fight it.
      paint(node, node.dataset.id ?? "")
    }
    const end = () => {
      const settled = swipes.end()
      const el = node
      node = null
      if (!settled || !el) return
      paint(el, settled.id)
    }
    scroller.addEventListener("touchstart", start, { passive: true })
    scroller.addEventListener("touchmove", move, { passive: true })
    scroller.addEventListener("touchend", end, { passive: true })
    scroller.addEventListener("touchcancel", end, { passive: true })
    return () => {
      scroller.removeEventListener("touchstart", start)
      scroller.removeEventListener("touchmove", move)
      scroller.removeEventListener("touchend", end)
      scroller.removeEventListener("touchcancel", end)
      swipes.closeOpen()
    }
  }, [scrollRef])
  return swiped
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
      // One gesture has one owner. The swipe decides the axis from the first
      // real movement, and a gesture it has taken is not also a pull: a
      // diagonal drag used to open the pad and the row at once.
      if (swipes.axis() === "row") {
        distance = 0
        pad.style.height = "0px"
        return
      }
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
          taskReads.arrived()
          if (alive) setList(d.tasks ?? [])
        })
        // Not shown to anybody: a list that did not arrive costs an indent and
        // a chip, and a banner over that would be worse than the thing it
        // reports. But it is said once, by name, because the version of this
        // that said nothing at all is why a phone drew a flat list for as long
        // as it did (`session/task-read.ts`).
        .catch((error: unknown) => {
          taskReads.failed(error)
          if (alive) setList(null)
        })
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
