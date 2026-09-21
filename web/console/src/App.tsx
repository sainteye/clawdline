import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react"
import type { Icon, SessionRow } from "@clawdline/contract"
import { ClawdlineClient } from "@clawdline/core"
import { client } from "./client.js"
import { connectionLightState } from "./connection-state.js"
import { useFleet, usePoll } from "./useFleet.js"
import { SessionsPage } from "./Sessions.js"
import { toggleOrder } from "./session/Transcript.js"
import { conversationNotStarted } from "./session/readiness.js"
import * as L from "./legacy/bridge.js"
import type { PageModule } from "./pages/types.js"
import { drawerEntries, pageReady } from "./pages/registry.js"
import { pageFromHash } from "./page-route.js"
import { workWord } from "./pages/work/words.js"
import { nowWord } from "./pages/now/words.js"
import { nextWord } from "./next-strings.js"
import { namesSession, sessionFragment, sessionsInFragment } from "./session/address.js"
import { NewBuild } from "./NewBuild.js"
import { sessionCountState, sessionReadingChinese, totalSessionWords } from "./session-reading.js"
import {
  ActionConfirm,
  GO_PAGE,
  Info,
  OPEN_CONFIRM,
  OPEN_INFO,
  Overlays,
  closeKeys,
  endedIfGone,
  getClosingId,
  hostConfirm,
  hostInfo,
  shown,
  toast,
  toggleKeys,
  type ConfirmRequest,
  type PageRequest,
} from "./overlays/index.js"

/**
 * The shell: a wordmark that opens the pages, a connection light, and one page
 * at a time.
 *
 * Same elements and same classes as `Resources/web/index.html`, because the
 * stylesheet is that app's. The drawer is `input/sidebar.js` and the page
 * switch is `core/pages.js`, rule for rule.
 */
type Page = "sessions" | "devices" | "projects" | "usage" | "ledger" | "timeline" | "plan" | "settings" | "work" | "now"

/** What became of a session the address asked for: see `openAsked`. */
type Asked = "none" | "waiting" | "opened" | "gone"

// The drawer's rows as `index.html` has them: its order and its ids. Pages
// whose backend this daemon does not own stay on screen and
// disabled rather than missing, so what is not here can be seen.
//
// `usage-open` and `nav-ledger` are hidden in the markup and shown by that same
// `apply` on a board answer that carries `enabled: false` — Board mode off,
// which is the mode this daemon is always in: it has no Board switch, and its
// `/v1/board` carries no `enabled` at all. So the two rows are shown here, as
// the original shows them in the mode this console is in, and the drawer holds
// the seven pages the original's holds.
//
// `nav-ledger` keeps the markup's English over there — `core/dom.js` has no
// such id in its element table, so `static.js`'s paint of `T.webLedger` writes
// to nothing — and does not here, because this drawer is React's and reads the
// catalog like every other row.
//
// The Timeline is in neither drawer: it is one work item's Project history,
// reached from that item on the work page.
const PAGES: { id: Page; nav: string; key?: string; text?: string; ready: boolean }[] = [
  { id: "sessions", nav: "nav-sessions", key: "webSessions", ready: true },
  { id: "devices", nav: "nav-devices", key: "webDevices", ready: false },
  { id: "projects", nav: "nav-projects", key: "webProjects", ready: false },
  { id: "usage", nav: "usage-open", key: "webUsage", ready: false },
  { id: "ledger", nav: "nav-ledger", key: "webLedger", ready: false },
  { id: "plan", nav: "nav-plan", key: "webPlan", ready: false },
  { id: "settings", nav: "nav-settings", key: "webSettings", ready: false },
]

// The wordmark's mark, `main.js`'s literal: the project's own creature, drawn
// by the code the rows use, at 3px a cell. The door draws it too (door/Door.tsx).
export const BRAND_MARK: Icon = {
  accent: "#d97757",
  cells: [".######.", ".#o##o#.", "########", ".##..##."].map((row) =>
    row.split("").map((ch) => (ch === "#" ? "#d97757" : ch === "o" ? "#141416" : "#33201a")),
  ),
}

/**
 * Pages built as their own files. Any `pages/*.tsx` exporting `page` is found
 * here at build time; its id turns its drawer item on. See pages/types.ts.
 */
const PAGE_MODULES: Record<string, PageModule> = Object.fromEntries(
  Object.values(import.meta.glob<{ page?: PageModule }>("./pages/*.tsx", { eager: true }))
    .map((m) => m.page)
    .filter((m): m is PageModule => !!m)
    .map((m) => [m.id, m]),
)

/** Whether this console can show a page: built in, or registered in pages/. */
function ready(id: string): boolean {
  return pageReady(id, PAGES.some((p) => p.id === id && p.ready), PAGE_MODULES)
}

/** The pages a fragment may name here: `Pages.knows`, less the ones this daemon cannot show. */
function knows(name: string): name is Page {
  return ready(name)
}

/**
 * `writeHash` (`main.js`): with `replaceState`, because a page is where you are
 * and not a step you took, and the fragment is the whole of it.
 */
function writeHash(hash: string): void {
  try {
    history.replaceState(history.state, "", hash)
  } catch {
    location.hash = hash
  }
}

/** The address of the list: the page's own, with no fragment. */
function listAddress(): string {
  return location.pathname + location.search
}

/** Back to the list's address, in place: no Back step is added. */
function leaveAddress(state: unknown): void {
  try {
    history.replaceState(state, "", listAddress())
  } catch {
    try {
      location.hash = ""
    } catch {
      /* nothing more to do */
    }
  }
}

/**
 * The phone's detail is one step above the list, so that the back gesture means
 * what it looks like (`openSession`, `session/open.js`), and the step carries
 * the session's address.
 *
 * There is only ever the one step. Opening a session from a detail — a forward
 * gesture, a reload of a detail, a link to another session — replaces the step
 * that is there; one pushed over it would leave Back going to a session that
 * is no longer on screen. An address that arrived asking for a session, typed
 * or tapped in a notification, had no list under it: the entry it arrived in
 * becomes the list and the detail is pushed over that, so Back from it is the
 * list rather than whatever came before the console.
 */
function stepIntoDetail(id: string, address: string, arrived: boolean): void {
  const detail = { view: "detail", id }
  try {
    if ((history.state as { view?: unknown } | null)?.view === "detail") {
      history.replaceState(detail, "", address)
    } else if (arrived) {
      history.replaceState({ view: "list" }, "", listAddress())
      history.pushState(detail, "", address)
    } else {
      history.pushState(detail, "", address)
    }
  } catch {
    /* the address stays as it was */
  }
}

/** `phone()` (`core/env.js`): the width the stylesheet switches at, asked each time. */
const phone = () => window.matchMedia("(max-width: 899px)").matches
const reduced = !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches

/** `releaseKeyboardFocus` (`core/env.js`): whatever holds the caret lets go before a phone screen is replaced. */
function releaseKeyboardFocus(): void {
  const el = document.activeElement as HTMLElement | null
  if (el && el !== document.body && typeof el.blur === "function") el.blur()
}

/** `typing` (`input/keys.js`). */
function typing(el: Element | null): el is HTMLElement {
  return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || (el as HTMLElement).isContentEditable)
}

/** A row's node, as `rowNodes` holds it there. */
function rowNode(id: string): HTMLElement | null {
  for (const node of document.querySelectorAll<HTMLElement>("#rows > li.row")) {
    if (node.dataset.id === id) return node
  }
  return null
}

/**
 * `aside` is drawn in the header between the counts and the connection light.
 * The daemon's console passes nothing; a console reading a machine through
 * Clawdline Cloud puts which machine it is there (`cloud/CloudGate.tsx`).
 */
export default function App({ aside }: { aside?: ReactNode } = {}) {
  const fleet = useFleet(client)
  const [page, setPage] = useState<Page>("sessions")
  const [menu, setMenu] = useState(false)
  const [, setLoaded] = useState(0)
  const brandRef = useRef<HTMLButtonElement>(null)
  const sidebarRef = useRef<HTMLElement>(null)
  const markRef = useRef<HTMLCanvasElement>(null)
  // A page the address asks for on arrival is reached while the body is still
  // hidden for its words, and nothing hidden can take the keyboard; the landing
  // waits for the words.
  const landOnBrand = useRef(false)

  // The session list's own arrangement, which the original keeps in
  // `SessionSelection` and on `main#app`: the highlight and the open session
  // are two things (`li.selected`, `li.open`), `data-view` is the phone's one
  // screen at a time, and `data-pane` is the desk's second column on or off.
  // Each is mirrored in a ref because the keyboard reads them between renders,
  // as the original reads its state object.
  const [selected, setSelectedState] = useState<string | null>(null)
  const [openId, setOpenState] = useState<string | null>(null)
  const [view, setViewState] = useState<"list" | "detail">("list")
  const [paneOpen, setPaneState] = useState(true)
  const [filter, setFilterState] = useState("")
  const pageRef = useRef<Page>(page)
  const menuRef = useRef(menu)
  menuRef.current = menu
  const selectedRef = useRef(selected)
  const openRef = useRef(openId)
  const viewRef = useRef(view)
  const paneRef = useRef(paneOpen)
  const filterRef = useRef(filter)
  const setSelected = (id: string | null) => {
    selectedRef.current = id
    setSelectedState(id)
  }
  const setOpen = (id: string | null) => {
    openRef.current = id
    setOpenState(id)
  }
  const setView = (to: "list" | "detail") => {
    viewRef.current = to
    setViewState(to)
  }
  const setPane = (on: boolean) => {
    paneRef.current = on
    setPaneState(on)
  }
  const setFilter = (q: string) => {
    filterRef.current = q
    setFilterState(q)
  }

  // The catalog, from the slot the daemon filled if it filled one, and from
  // /v1/strings if it did not. Either way the page is uncovered afterwards,
  // whatever the outcome: a console that stayed hidden because a translation
  // failed would be worse than one that starts in English.
  useEffect(() => {
    const inline = (window as { __strings?: Record<string, string> }).__strings
    const get = async () => inline ?? (await client.strings())
    void L.loadStrings(get, () => setLoaded((n) => n + 1)).finally(() => {
      document.documentElement.classList.remove("booting")
      if (landOnBrand.current) {
        landOnBrand.current = false
        brandRef.current?.focus({ preventScroll: true })
      }
      setLoaded((n) => n + 1)
    })
  }, [])

  useLayoutEffect(() => {
    L.paintIcon(markRef.current, BRAND_MARK, 3)
  }, [])

  // `Sidebar.close`: where the focus is is read before the drawer goes, because
  // hiding the focused row drops focus on the body, from where nothing can give
  // it back. Only when it was in here: closing behind a choice finds focus
  // already where the new page put it.
  const closeMenu = useCallback(() => {
    const drawer = sidebarRef.current
    if (!drawer || drawer.hidden) return
    const held = drawer.contains(document.activeElement)
    if (held) brandRef.current?.focus({ preventScroll: true })
    setMenu(false)
  }, [])

  // `Sidebar.open`: the current page's row takes the keyboard, so a screen
  // reader is told where it is.
  useLayoutEffect(() => {
    if (!menu) return
    const here = sidebarRef.current?.querySelector<HTMLElement>('[aria-current="page"]')
    here?.focus({ preventScroll: true })
  }, [menu])

  // `Pages.go`: asking for the page already on screen does nothing at all, the
  // drawer included. A real move lands the keyboard on the wordmark (the
  // registry's `focusFallback`, since neither page here names a control of its
  // own), writes the page into the address unless the address is what asked,
  // and then closes the drawer behind it (`markSidebarPage`). The page left is
  // hidden, not taken down: `main#app` keeps its scroll and its open session —
  // so coming back to it writes that session's address, not the page's.
  const go = (to: Page, options?: { hash?: boolean }) => {
    if (!knows(to) || to === pageRef.current) return false
    pageRef.current = to
    setPage(to)
    if (document.documentElement.classList.contains("booting")) landOnBrand.current = true
    else brandRef.current?.focus({ preventScroll: true })
    if (options?.hash !== false) {
      writeHash(to === "sessions" && openRef.current ? sessionFragment(openRef.current) : "#page=" + encodeURIComponent(to))
    }
    closeMenu()
    return true
  }

  // `Pages.bind` writes the answer on the root element, and `routeTo`
  // (`input/route.js`) follows the address: once on arrival and on every
  // change, without writing back the address it was just read from. A page
  // this daemon cannot show is not a page here, as an unknown name is not one
  // there. A session the address names is held until the list has it
  // (`openAsked` below): on arrival the list has not come yet.
  useLayoutEffect(() => {
    document.documentElement.setAttribute("data-page", page)
  }, [page])
  const goRef = useRef(go)
  goRef.current = go
  const askedRef = useRef<string[] | null>(null)
  const openAskedRef = useRef<() => Asked>(() => "none")
  useEffect(() => {
    const routeTo = () => {
      goRef.current(pageFromHash(location.hash, knows), { hash: false })
      askedRef.current = sessionsInFragment(location.hash)
      if (askedRef.current) openAskedRef.current()
    }
    routeTo()
    window.addEventListener("hashchange", routeTo)
    return () => window.removeEventListener("hashchange", routeTo)
  }, [])

  const rows = fleet.snapshot?.sessions ?? []
  const rowsRef = useRef(rows)
  rowsRef.current = rows
  const snapshotRef = useRef(fleet.snapshot)
  snapshotRef.current = fleet.snapshot
  const T = L.strings

  // `select` (`session/agent.js`): the highlight moves, and the keyboard with it.
  const select = (id: string) => {
    if (!rowsRef.current.some((r) => r.id === id)) return
    setSelected(id)
    const node = rowNode(id)
    if (node) {
      node.focus({ preventScroll: true })
      node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" })
    }
  }

  // `move`: through the list as it is drawn, from the highlight, or from an
  // end when there is none.
  const move = (delta: number) => {
    const list = L.orderedRows()
    if (!list.length) return
    const at = selectedRef.current ? list.findIndex((r) => r.id === selectedRef.current) : -1
    const next = at < 0 ? (delta > 0 ? 0 : list.length - 1) : Math.min(list.length - 1, Math.max(0, at + delta))
    select(list[next].id)
  }

  // While a session is being ended, and until that settles, the open session is
  // not given back or replaced (`closingSelectionKey`). The confirmation owns
  // the close and is asked first; the header's own flag is read as well while
  // the header still runs a close of its own.
  const closing = () =>
    getClosingId() !== null || document.getElementById("detail-head")?.dataset.closing === "on"

  // `openSession` (`session/open.js`). A session lives on the sessions page, so
  // opening one means being there. Either way the address names it, so a
  // reload or a copied link comes back to it. On a phone it is a whole screen,
  // with a history entry so the back gesture means what it looks like
  // (`stepIntoDetail`); on a desk it puts the second column back if it was put
  // away, and the address is replaced, because moving between sessions there
  // is where you are and not a step you took. `keepFocus` is the first list's
  // courtesy open, which must not take the keyboard; `arrived` is an address
  // that asked for this session.
  const openSession = (id: string, keepFocus = false, arrived = false) => {
    if (!rowsRef.current.some((r) => r.id === id)) return
    if (closing() && openRef.current === id) return
    go("sessions")
    setSelected(id)
    setOpen(id)
    if (phone()) {
      if (viewRef.current !== "detail") releaseKeyboardFocus()
      setView("detail")
      stepIntoDetail(id, listAddress() + sessionFragment(id), arrived)
    } else {
      if (!paneRef.current) setPane(true)
      writeHash(sessionFragment(id))
    }
    if (!keepFocus && !phone()) rowNode(id)?.focus({ preventScroll: true })
  }

  // The session the address asked for (`openWanted`, `input/route.js`), once
  // the list has it. Until a list has arrived it waits; a list read in full
  // that does not have it means it has gone, and that is said — the address
  // is put back to what is on screen and nothing else is opened in its place.
  // A reading that did not complete may not have reached it yet, so it waits
  // for the next one.
  const openAsked = (): Asked => {
    const asked = askedRef.current
    const snapshot = snapshotRef.current
    if (!asked) return "none"
    if (!snapshot) return "waiting"
    const id = asked.find((c) => snapshot.sessions.some((r) => r.id === c))
    if (id) {
      askedRef.current = null
      openSession(id, false, true)
      return "opened"
    }
    if (!snapshot.scan.complete) return "waiting"
    askedRef.current = null
    if (openRef.current) writeHash(sessionFragment(openRef.current))
    else leaveAddress(history.state)
    toast(nextWord("sessionGone"))
    return "gone"
  }
  openAskedRef.current = openAsked

  // `closeDetail`: the session goes, the highlight stays. On a phone the list
  // comes back and a `#session=…` or `#page=…` address goes with the detail,
  // replaced rather than pushed so it adds no Back step. On a desk the list
  // never went, and only a `#session=…` address goes: one naming another page
  // is where the reader is.
  // `settled` is the close's own call: it gives the detail back once the close
  // has settled, a frame before the header has been redrawn to say so.
  const closeDetail = (settled = false) => {
    if (!settled && openRef.current && closing()) return
    setOpen(null)
    if (phone()) {
      setView("list")
      leaveAddress({ view: "list" })
    } else if (namesSession(location.hash)) {
      leaveAddress(history.state)
    }
  }

  // What each list does to the selection (`handlers.js`'s `apply`,
  // `SessionSelection.reconcile`): a session that went away takes its
  // highlight and its detail with it. A session the address asked for is
  // looked for in every list until it is found or known gone. The first list
  // to arrive puts the highlight on the top row; on a desk it also opens it,
  // on a phone it does not — and not when the address asked for another page,
  // or for a session: that is answered by the session or by saying it has
  // gone, never by opening the top one in its place.
  const firstList = useRef(true)
  useEffect(() => {
    // A list without the session being closed is that close's answer
    // (`SessionActions.gone`), and it settles before the detail is given back.
    endedIfGone(new Set(rows.map((r) => r.id)))
    if (selectedRef.current && !rows.some((r) => r.id === selectedRef.current)) setSelected(null)
    if (openRef.current && !rows.some((r) => r.id === openRef.current)) closeDetail()
    const asked = openAsked()
    if (firstList.current && (rows.length || asked === "gone")) {
      firstList.current = false
      if (pageRef.current === "sessions" && asked !== "opened") {
        const top = L.orderedRows()[0]
        if (top) {
          if (phone() || asked !== "none") setSelected(top.id)
          else openSession(top.id, true)
        }
      }
    }
  }, [rows])

  // `Info.follow` after every list and every change of the open session: a
  // card about a session that is no longer open closes, and one whose session
  // changed state is redrawn.
  useEffect(() => {
    Info.follow()
  }, [rows, openId])

  // The phone's back gesture (`input/action-confirm.js`), and a window that
  // changes width under an open session (`input/edges.js`).
  const layoutRef = useRef({ closeDetail, refresh: fleet.refresh })
  layoutRef.current = { closeDetail, refresh: fleet.refresh }

  // What the overlays ask of the page. `writable` is the original's `S.write`,
  // which comes from `/v1/health`; this daemon's health does not carry it, and
  // the composer already takes the page as writable until a send is refused.
  useEffect(() => {
    hostInfo({ openId: () => openRef.current, writable: () => true })
    hostConfirm({
      openId: () => openRef.current,
      writable: () => true,
      closeDetail: () => layoutRef.current.closeDetail(true),
      refresh: () => layoutRef.current.refresh(),
    })
  }, [])

  // The presses that open an overlay from a component that does not own it
  // (`overlays/events.ts`). `#detail-info` and the menu's Session info row
  // open nothing while no session is open, as there.
  // A page asked for by a component that does not own the router
  // (`requestPage`, overlays/events.ts): `Pages.go` with the options it was
  // given, and no address written when it said not to. The Documents page asks
  // this way because `main.js` does, and because writing the address is a
  // same-document navigation that the phone's back gesture below cannot tell
  // from a gesture.
  useEffect(() => {
    const onGo = (ev: Event) => {
      const want = (ev as CustomEvent<PageRequest | undefined>).detail
      if (!want?.page || !knows(want.page)) return
      goRef.current(want.page, { hash: want.hash })
    }
    document.addEventListener(GO_PAGE, onGo)
    return () => document.removeEventListener(GO_PAGE, onGo)
  }, [])

  useEffect(() => {
    const onInfo = () => {
      if (openRef.current) Info.open()
    }
    const onConfirm = (ev: Event) => {
      const ask = (ev as CustomEvent<ConfirmRequest | undefined>).detail
      ActionConfirm.open(ask?.kind || "end", ask?.id, ask?.opener, undefined, {
        subject: ask?.subject,
        focus: ask?.focus,
      })
    }
    document.addEventListener(OPEN_INFO, onInfo)
    document.addEventListener(OPEN_CONFIRM, onConfirm)
    return () => {
      document.removeEventListener(OPEN_INFO, onInfo)
      document.removeEventListener(OPEN_CONFIRM, onConfirm)
    }
  }, [])
  useEffect(() => {
    const onPop = () => {
      if (!phone()) return
      if (viewRef.current === "detail") layoutRef.current.closeDetail()
    }
    const onResize = () => {
      if (!phone()) setView("list")
      else if (openRef.current) setView("detail")
    }
    window.addEventListener("popstate", onPop)
    window.addEventListener("resize", onResize)
    return () => {
      window.removeEventListener("popstate", onPop)
      window.removeEventListener("resize", onResize)
    }
  }, [])

  // `input/keys.js`, one listener, in its order: one press does one thing, and
  // a `return` here ends this listener and nothing else. Of the sheets it asks
  // about, the confirmation, Info and the keyboard card are here; the door,
  // Start, Command, Settings and the schedule form are not, and neither are an
  // agent's transcript or the pages with a step inside them; `r` has nothing
  // to reverse here. The session menu answers its own Escape before this sees
  // it, as `detail-actions.js` does.
  const onKey = (ev: KeyboardEvent) => {
    const key = ev.key
    const meta = ev.metaKey || ev.ctrlKey
    const rowsEl = document.getElementById("rows")
    const filterEl = document.getElementById("filter") as HTMLInputElement | null

    // A confirmation is a decision about one action, not another layer of the
    // page: while it is open nothing behind it runs, and Escape is the way out.
    if (ActionConfirm.isOpen()) {
      if (key === "Escape") {
        ev.preventDefault()
        ActionConfirm.close(true)
      }
      return
    }

    if (meta && (key === "k" || key === "K")) {
      ev.preventDefault()
      rowsEl?.focus()
      if (!selectedRef.current) move(1)
      else select(selectedRef.current)
      return
    }
    if (meta && (key === "j" || key === "J")) {
      ev.preventDefault()
      if (phone()) {
        setView(viewRef.current === "detail" ? "list" : "detail")
        return
      }
      setPane(!paneRef.current)
      return
    }
    if (meta && (key === "i" || key === "I")) {
      ev.preventDefault()
      if (Info.isOpen()) {
        Info.close()
        return
      }
      // One sheet is not stacked over another: an open drawer or keyboard
      // card owns the next key until it closes.
      if (!menuRef.current && !shown("keys")) Info.open()
      return
    }

    if (key === "Escape") {
      if (Info.isOpen()) {
        Info.close()
        return
      }
      // The drawer is over whatever page is showing, so it goes before the page does.
      if (menuRef.current) {
        closeMenu()
        return
      }
      if (shown("keys")) {
        closeKeys()
        return
      }
      if (pageRef.current !== "sessions") {
        if (document.querySelector("dialog[open]")) return
        go("sessions")
        return
      }
      const active = document.activeElement
      if (filterEl && active === filterEl) {
        if (filterRef.current) {
          filterEl.value = ""
          setFilter("")
        } else {
          filterEl.blur()
          rowsEl?.focus()
        }
        return
      }
      if (typing(active)) {
        active.blur()
        return
      }
      if (openRef.current) closeDetail()
      return
    }

    if (typing(document.activeElement)) return
    if (meta || ev.altKey) return
    // A sheet is over the page, so `j` is not "move down the list behind it",
    // and the drawer is one more thing that is over it (keys.js). Settings is
    // the one such sheet this console has so far.
    if (menuRef.current || pageRef.current === "settings") return

    switch (key) {
      case "ArrowDown":
      case "j":
        ev.preventDefault()
        move(1)
        break
      case "ArrowUp":
      case "k":
        ev.preventDefault()
        move(-1)
        break
      case "Enter":
        if (selectedRef.current) {
          ev.preventDefault()
          openSession(selectedRef.current)
        }
        break
      case "/":
        ev.preventDefault()
        filterEl?.focus()
        filterEl?.select()
        break
      case "g": {
        ev.preventDefault()
        const tx = document.getElementById("tx-scroll")
        if (tx) tx.scrollTop = 0
        break
      }
      case "G": {
        ev.preventDefault()
        const tx = document.getElementById("tx-scroll")
        if (tx) tx.scrollTop = tx.scrollHeight
        break
      }
      case "r":
        ev.preventDefault()
        toggleOrder()
        break
      case "?":
        ev.preventDefault()
        toggleKeys()
        break
      default:
        break
    }
  }
  const keyRef = useRef(onKey)
  keyRef.current = onKey
  useEffect(() => {
    const listener = (ev: KeyboardEvent) => keyRef.current(ev)
    document.addEventListener("keydown", listener)
    return () => document.removeEventListener("keydown", listener)
  }, [])

  return (
    <>
      <header className="top">
        <button
          className="brand"
          id="brand"
          type="button"
          aria-label={T.webMenu}
          title={T.webMenu}
          aria-controls="sidebar"
          aria-expanded={menu}
          ref={brandRef}
          onClick={() => (menu ? closeMenu() : setMenu(true))}
        >
          <canvas id="brand-mark" width={0} height={0} ref={markRef} />
          <b>clawdline</b>
        </button>
        <Counts reading={sessionCountState(fleet)} recovering={!!fleet.snapshot && !fleet.snapshot.scan.complete} />
        {aside}
        <Conn live={fleet.live} onRetry={fleet.refresh} />
      </header>

      {/* The dark half is the way out, told from a row by what was hit — not by
          the panel stopping its own clicks (`input/sidebar.js`). */}
      <nav
        className="sidebar"
        id="sidebar"
        hidden={!menu}
        aria-label={T.webPages}
        ref={sidebarRef}
        onClick={(ev) => {
          if (ev.target === ev.currentTarget) closeMenu()
        }}
        // A press on a row this daemon cannot open is not a way out of the
        // drawer: left alone, the browser takes focus off the current row and
        // drops it on the body, from where the keyboard has nowhere to be. A
        // disabled button is sent pointer events and no mouse ones, so this is
        // the press to cancel.
        onPointerDownCapture={(ev) => {
          if ((ev.target as Element).closest?.("button:disabled")) ev.preventDefault()
        }}
      >
        <div className="sidebar-panel" id="sidebar-panel">
          {drawerEntries(PAGES, PAGE_MODULES).map((p) => (
            <button
              key={p.id}
              className="sidebar-item"
              id={p.nav}
              type="button"
              data-page-to={p.id}
              aria-current={page === p.id ? "page" : undefined}
              disabled={!ready(p.id)}
              onClick={() => go(p.id)}
            >
              {p.key ? T[p.key] : p.text}
            </button>
          ))}
          {/* The work system (design-decisions T6): not one of the retired app's
              pages, so it remains a row of its own at the end. */}
          <button
            className="sidebar-item"
            id="nav-work"
            type="button"
            data-page-to="work"
            aria-current={page === "work" ? "page" : undefined}
            disabled={!ready("work")}
            onClick={() => go("work")}
          >
            {workWord("nav")}
          </button>
          {/* "Where things stand" (work-system-review §5.2, W4): the one page
              that answers the question the person asked three times in a day.
              Last, beside the board, because it is a reading and not a place
              work is done — nothing on it can be changed from it. */}
          <button
            className="sidebar-item"
            id="nav-now"
            type="button"
            data-page-to="now"
            aria-current={page === "now" ? "page" : undefined}
            disabled={!ready("now")}
            onClick={() => go("now")}
          >
            {nowWord("nav")}
          </button>
        </div>
      </nav>

      <SessionsPage
        rows={rows}
        loaded={fleet.loaded}
        arrived={fleet.snapshot !== null}
        live={fleet.live}
        emptyAuthoritative={fleet.snapshot?.scan.emptyAuthoritative ?? false}
        readingSource={fleet.snapshot?.scan.source}
        scanNotes={fleet.snapshot?.scan.notes}
        scanSources={fleet.snapshot?.scan.sources}
        shown={page === "sessions"}
        view={view}
        paneOpen={paneOpen}
        filter={filter}
        onFilter={setFilter}
        selected={selected}
        openId={openId}
        onOpen={(id) => openSession(id)}
        onBack={() => closeDetail()}
        onDid={fleet.refresh}
      />
      {Object.values(PAGE_MODULES).map(({ id, Component }) => (
        // Mounted once opened and kept, as the original keeps its sections in
        // the document and only hides them.
        <Component key={id} shown={page === id} />
      ))}
      <Overlays />
      <NewBuild />
    </>
  )
}

/**
 * The count beside the wordmark, `renderCounts` (`view/list.js`).
 *
 * What is working leads, what is waiting is louder, shells running are counted
 * so that "all quiet" cannot be said over a build, and what could not be read is
 * quiet but present. The rows on this wire carry no `shells` yet; the part is
 * read from the field the original reads, so it appears when the field does.
 *
 * The sync parts are the original's `S.sessionSync`: a machine still sending
 * its rows, or one that could not. This daemon reads one machine, and a scan
 * that did not complete is the same fact — the count is not the whole fleet —
 * so it takes the `recovering` part. It reports no per-machine failure, so the
 * `failures` part has no input here; when it does, its words need
 * `describeFailure` (`core/failure-text.js`) from the bridge.
 */
function Counts({
  reading,
  recovering,
}: {
  reading: ReturnType<typeof sessionCountState>
  recovering: boolean
}) {
  const T = L.strings
  const rows = reading.phase === "ready" ? reading.value : []
  let working = 0
  let waiting = 0
  let notStarted = 0
  let unknown = 0
  let shells = 0
  for (const s of rows) {
    if (s.state === "working") working++
    else if (s.state === "waiting") waiting++
    else if (conversationNotStarted(s)) notStarted++
    else if (s.state === "unknown") unknown++
    shells += (s.shells ?? []).length
  }
  const bits: { cls: string; text: string }[] = []
  if (reading.phase === "ready") bits.push({ cls: "part quiet", text: totalSessionWords(reading.value.length) })
  else if (reading.phase === "empty_authoritative") bits.push({ cls: "part quiet", text: totalSessionWords(0) })
  else if (reading.phase === "refused") {
    bits.push({ cls: "part quiet", text: L.failureSentence(reading.error, nextWord("sessionsListUnansweredTitle")) })
  } else if (reading.phase === "unanswered") {
    bits.push({ cls: "part quiet", text: nextWord("sessionsListUnansweredTitle") })
  } else {
    bits.push({ cls: "part quiet", text: nextWord("sessionsListWaitTitle") })
  }
  if (working) bits.push({ cls: "part", text: L.fillString(T.webCountWorking, { n: working }) })
  if (waiting) bits.push({ cls: "part waiting", text: L.fillString(T.webCountWaiting, { n: waiting }) })
  if (notStarted) bits.push({ cls: "part quiet", text: nextWord("sessionCountNotStarted", { n: notStarted }) })
  if (shells) {
    bits.push({
      cls: "part quiet",
      text: shells === 1 ? T.sessionShellOne : L.fillString(T.sessionShellMany, { n: shells }),
    })
  }
  if (unknown) bits.push({ cls: "part quiet", text: L.fillString(T.webCountUnreadable, { n: unknown }) })
  // The line is up — this band is drawn from a snapshot that arrived — so the
  // sentence is about the list, not about the app (`Sessions.tsx`'s empty
  // state, and `next-strings.ts` on why these are two words and not one).
  if (recovering && reading.phase === "ready") bits.push({ cls: "part quiet", text: nextWord("sessionsListWaitTitle") })
  if (rows.length > 0 && !working && !waiting && !unknown && !shells && !recovering) {
    bits.push({ cls: "part quiet", text: sessionReadingChinese() ? "都很安靜" : "all quiet" })
  }
  return (
    <div className="counts" id="counts">
      {bits.map((b, i) => (
        <span key={i} className={b.cls}>
          {b.text}
        </span>
      ))}
    </div>
  )
}

/**
 * The connection light, `renderConn` (`view/list.js`).
 *
 * The tip is "streaming from the app · version" when live and "not connected —
 * press to retry" otherwise, and pressing it asks again (`detail-actions.js`).
 * The original's version comes from its hello frame; this daemon's `/v1/health`
 * carries none, so the tip ends where the version would start. It is read from
 * the field the original reads, so it appears when the field does.
 */
function Conn({ live, onRetry }: { live: boolean; onRetry: () => void }) {
  const read = useMemo(() => () => client.health(), [])
  const { data, error: healthError } = usePoll(read, 15000)
  const T = L.strings
  // The stream says the browser's line is open; health says the selected host
  // answered. In Cloud those are different subjects, so the relay opening
  // must not paint "connected" before the chosen machine's health arrives.
  const state = connectionLightState(live, data !== null, healthError !== null)
  const label =
    state === "offline" ? T.webConnOffline : state === "live" ? T.webConnLive : T.webConnConnecting
  const version = (data as unknown as { version?: unknown } | null)?.version
  const tip =
    state === "live"
      ? T.webConnTipLive + (typeof version === "string" && version ? " · " + version : "")
      : T.webConnTipDown
  return (
    <button className="conn" id="conn" data-state={state} title={tip} onClick={onRetry}>
      <span className="dot" />
      <span id="conn-label">{label || state}</span>
    </button>
  )
}

export { ClawdlineClient }
