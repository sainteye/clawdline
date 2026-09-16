import { useEffect, useState } from "react"
import { ClawdlineClient } from "@clawdline/core"
import { client } from "./client.js"
import { useFleet, usePoll } from "./useFleet.js"
import { SessionsPage } from "./Sessions.js"
import Dashboard from "./Dashboard.js"
import * as L from "./legacy/bridge.js"
import { useMemo } from "react"

/**
 * The shell: a wordmark that opens the pages, a connection light, and one page
 * at a time.
 *
 * Same elements and same classes as `Resources/web/index.html`, because the
 * stylesheet is that app's. The sidebar lists the pages the original lists; the
 * ones whose backend this daemon does not own are present and disabled rather
 * than missing, so what is not here can be seen instead of guessed at.
 *
 * The dashboard is the one entry that is not in the original. It was this
 * console for a while and it is a different idea — the fleet as the subject
 * rather than the conversation — so it is kept, as a page rather than as the
 * app.
 */
type Page = "sessions" | "dashboard" | "devices" | "projects" | "usage" | "ledger" | "plan" | "settings"

// The pages the original's drawer lists, in its order, under its names. The
// ones whose backend this daemon does not own are present and disabled rather
// than missing: what is not here should be visible, not guessed at.
const PAGES: { id: Page; key: string; ready: boolean }[] = [
  { id: "sessions", key: "webSessions", ready: true },
  { id: "devices", key: "webDevices", ready: false },
  { id: "projects", key: "webProjects", ready: false },
  { id: "usage", key: "webUsage", ready: false },
  { id: "ledger", key: "webLedger", ready: false },
  { id: "plan", key: "webPlan", ready: false },
  { id: "settings", key: "webSettings", ready: false },
]

export default function App() {
  const fleet = useFleet(client)
  const [page, setPage] = useState<Page>("sessions")
  const [menu, setMenu] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [, setLoaded] = useState(0)

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
          onClick={() => setMenu((v) => !v)}
        >
          <b>clawdline</b>
        </button>
        <Counts rows={rows} />
        <Conn live={fleet.live} error={fleet.error} />
      </header>

      <nav className="sidebar" id="sidebar" hidden={!menu} aria-label="Pages">
        <div className="sidebar-panel" id="sidebar-panel">
          {/* Not one of the original's pages. It was this console for a while
              and it answers a different question — the fleet as the subject
              rather than the conversation — so it is kept, marked as the
              addition it is rather than slipped in among the others. */}
          <button
            className="sidebar-item"
            type="button"
            data-page-to="dashboard"
            onClick={() => {
              setPage("dashboard")
              setMenu(false)
            }}
          >
            Dashboard
          </button>
          {PAGES.map((p) => (
            <button
              key={p.id}
              className="sidebar-item"
              type="button"
              data-page-to={p.id}
              disabled={!p.ready}
              onClick={() => {
                setPage(p.id)
                setMenu(false)
              }}
            >
              {T[p.key] || p.id}
            </button>
          ))}
        </div>
      </nav>

      {page === "sessions" && (
        <SessionsPage
          rows={rows}
          loaded={fleet.loaded}
          emptyAuthoritative={fleet.snapshot?.scan.emptyAuthoritative ?? false}
          error={fleet.error}
          selected={selected}
          onSelect={setSelected}
          onDid={fleet.refresh}
        />
      )}
      {page === "dashboard" && <Dashboard fleet={fleet} />}
      {page !== "sessions" && page !== "dashboard" && (
        <section className="page">
          {/* Not from the catalog: the original has no such string because all
              of its pages exist. Saying so plainly beats borrowing a key that
              means something else. */}
          <div className="empty">這一頁的後端還沒做。</div>
        </section>
      )}
    </>
  )
}

/**
 * The count beside the wordmark.
 *
 * The parts and their order are `renderCounts`'s: what is working leads, what
 * is waiting is louder, and what could not be read is quiet but present —
 * counted so that "all quiet" cannot be said over a screen nobody could see.
 */
function Counts({ rows }: { rows: { state: string }[] }) {
  const T = L.strings
  let working = 0
  let waiting = 0
  let unknown = 0
  for (const s of rows) {
    if (s.state === "working") working++
    else if (s.state === "waiting") waiting++
    else if (s.state === "unknown") unknown++
  }
  const bits: { cls: string; text: string }[] = []
  if (working) bits.push({ cls: "part", text: L.fillString(T.webCountWorking, { n: working }) })
  if (waiting) bits.push({ cls: "part waiting", text: L.fillString(T.webCountWaiting, { n: waiting }) })
  if (unknown) bits.push({ cls: "part quiet", text: L.fillString(T.webCountUnreadable, { n: unknown }) })
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
 * The connection light.
 *
 * Three states, not two. A daemon that cannot be reached and a stream that is
 * not up are different facts, and the light says which.
 */
function Conn({ live, error }: { live: boolean; error: string | null }) {
  const read = useMemo(() => () => client.health(), [])
  const { data, error: healthError } = usePoll(read, 15000)
  const T = L.strings
  const state = healthError ? "offline" : live ? "live" : data ? "retrying" : "connecting"
  const label =
    state === "offline" ? T.webConnOffline : state === "live" ? T.webConnLive : T.webConnConnecting
  return (
    <button className="conn" id="conn" data-state={state} title={error ?? ""} type="button">
      <span className="dot" />
      <span id="conn-label">{label || state}</span>
    </button>
  )
}

export { ClawdlineClient }
