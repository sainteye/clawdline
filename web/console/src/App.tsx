import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import { ClawdlineClient } from "@clawdline/core"
import { client } from "./client.js"
import { useFleet, usePoll } from "./useFleet.js"
import { SessionsPage } from "./Sessions.js"
import Dashboard from "./Dashboard.js"
import * as L from "./legacy/bridge.js"

/**
 * The shell: a wordmark that opens the pages, a connection light, and one page
 * at a time.
 *
 * Same elements and same classes as `Resources/web/index.html`, because the
 * stylesheet is that app's. The drawer is `input/sidebar.js` and the page
 * switch is `core/pages.js`, rule for rule.
 *
 * The dashboard is the one entry that is not in the original. It was this
 * console for a while and it is a different idea — the fleet as the subject
 * rather than the conversation — so it is kept, as a page rather than as the
 * app.
 */
type Page = "sessions" | "dashboard" | "devices" | "projects" | "board" | "usage" | "ledger" | "plan" | "settings"

// The drawer's rows as `index.html` has them: its order, its ids, and its
// `hidden`. Pages whose backend this daemon does not own stay on screen and
// disabled rather than missing, so what is not here can be seen.
//
// `nav-board` is hidden by `BoardControls.apply` whatever the answer, and
// `static.js` never paints it, so it keeps the markup's English. `usage-open` and
// `nav-ledger` are hidden in the markup and shown only by a board answer that
// carries `enabled: false`; this daemon's `/v1/board` carries no `enabled`, which
// `apply` ignores, so they stay as the markup has them. `nav-ledger` also keeps
// the markup's English: `core/dom.js` has no such id in its element table, so
// `static.js`'s paint of `T.webLedger` writes to nothing there.
const PAGES: { id: Page; nav: string; key?: string; text?: string; ready: boolean; hidden?: boolean }[] = [
  { id: "sessions", nav: "nav-sessions", key: "webSessions", ready: true },
  { id: "devices", nav: "nav-devices", key: "webDevices", ready: false },
  { id: "projects", nav: "nav-projects", key: "webProjects", ready: false },
  { id: "board", nav: "nav-board", text: "Projects · Board", ready: false, hidden: true },
  { id: "usage", nav: "usage-open", key: "webUsage", ready: false, hidden: true },
  { id: "ledger", nav: "nav-ledger", text: "Verification ledger", ready: false, hidden: true },
  { id: "plan", nav: "nav-plan", key: "webPlan", ready: false },
  { id: "settings", nav: "nav-settings", key: "webSettings", ready: false },
]

// The wordmark's mark, `main.js`'s literal: the project's own creature, drawn
// by the code the rows use, at 3px a cell.
const BRAND_MARK: Icon = {
  accent: "#d97757",
  cells: [".######.", ".#o##o#.", "########", ".##..##."].map((row) =>
    row.split("").map((ch) => (ch === "#" ? "#d97757" : ch === "o" ? "#141416" : "#33201a")),
  ),
}

export default function App() {
  const fleet = useFleet(client)
  const [page, setPage] = useState<Page>("sessions")
  const [menu, setMenu] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [, setLoaded] = useState(0)
  const brandRef = useRef<HTMLButtonElement>(null)
  const sidebarRef = useRef<HTMLElement>(null)
  const markRef = useRef<HTMLCanvasElement>(null)

  // The catalog, from the slot the daemon filled if it filled one, and from
  // /v1/strings if it did not. Either way the page is uncovered afterwards,
  // whatever the outcome: a console that stayed hidden because a translation
  // failed would be worse than one that starts in English.
  useEffect(() => {
    const inline = (window as { __strings?: Record<string, string> }).__strings
    const get = async () => inline ?? (await client.strings())
    void L.loadStrings(get).finally(() => {
      document.documentElement.classList.remove("booting")
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

  // Escape closes the drawer before anything else on the page (`input/keys.js`):
  // it is over whatever page is showing, and one press closes one thing.
  useEffect(() => {
    if (!menu) return
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") closeMenu()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [menu, closeMenu])

  // `Pages.go`: asking for the page already on screen does nothing at all, the
  // drawer included. A real move lands the keyboard on the wordmark (the
  // registry's `focusFallback`, since neither page here names a control of its
  // own) and then closes the drawer behind it.
  const go = (to: Page) => {
    if (to === page) return
    setPage(to)
    brandRef.current?.focus({ preventScroll: true })
    closeMenu()
  }

  const rows = fleet.snapshot?.sessions ?? []
  const T = L.strings

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
        <Counts rows={rows} recovering={!!fleet.snapshot && !fleet.snapshot.scan.complete} />
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
      >
        <div className="sidebar-panel" id="sidebar-panel">
          {PAGES.map((p) => (
            <button
              key={p.id}
              className="sidebar-item"
              id={p.nav}
              type="button"
              data-page-to={p.id}
              aria-current={page === p.id ? "page" : undefined}
              hidden={p.hidden}
              disabled={!p.ready}
              onClick={() => go(p.id)}
            >
              {p.key ? T[p.key] : p.text}
            </button>
          ))}
          {/* Not one of the original's pages, so it comes after all of them.
              It was this console for a while and it answers a different
              question — the fleet as the subject rather than the conversation —
              and the user agreed to keep it. No catalog key names it. */}
          <button
            className="sidebar-item"
            id="nav-dashboard"
            type="button"
            data-page-to="dashboard"
            aria-current={page === "dashboard" ? "page" : undefined}
            onClick={() => go("dashboard")}
          >
            Dashboard
          </button>
        </div>
      </nav>

      {page === "sessions" && (
        <SessionsPage
          rows={rows}
          loaded={fleet.loaded}
          arrived={fleet.snapshot !== null}
          live={fleet.live}
          emptyAuthoritative={fleet.snapshot?.scan.emptyAuthoritative ?? false}
          selected={selected}
          onSelect={setSelected}
          onDid={fleet.refresh}
        />
      )}
      {page === "dashboard" && <Dashboard fleet={fleet} />}
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
function Counts({ rows, recovering }: { rows: { state: string; shells?: unknown[] }[]; recovering: boolean }) {
  const T = L.strings
  let working = 0
  let waiting = 0
  let unknown = 0
  let shells = 0
  for (const s of rows) {
    if (s.state === "working") working++
    else if (s.state === "waiting") waiting++
    else if (s.state === "unknown") unknown++
    shells += (s.shells ?? []).length
  }
  const bits: { cls: string; text: string }[] = []
  if (working) bits.push({ cls: "part", text: L.fillString(T.webCountWorking, { n: working }) })
  if (waiting) bits.push({ cls: "part waiting", text: L.fillString(T.webCountWaiting, { n: waiting }) })
  if (shells) {
    bits.push({
      cls: "part quiet",
      text: shells === 1 ? T.sessionShellOne : L.fillString(T.sessionShellMany, { n: shells }),
    })
  }
  if (unknown) bits.push({ cls: "part quiet", text: L.fillString(T.webCountUnreadable, { n: unknown }) })
  if (recovering) bits.push({ cls: "part quiet", text: T.webEmptyWaitTitle })
  if (!bits.length) {
    const quiet = rows.length
      ? L.fillString(rows.length === 1 ? T.webCountQuietOne : T.webCountQuietMany, { n: rows.length })
      : T.webCountNone
    bits.push({ cls: "part quiet", text: quiet })
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
  const state = healthError ? "offline" : live ? "live" : data ? "retrying" : "connecting"
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
