import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import {
  SECTIONS,
  answerDecision,
  answerProposal,
  command,
  createItem,
  readBacklog,
  readBoard,
  readDecisions,
  readDigests,
  readProjectPlaces,
  readProposals,
  resolveProposal,
  type BacklogPage,
  type Command,
  type Item,
  type ProjectPlace,
} from "./work/api.js"
import { BacklogView } from "./work/Backlog.js"
import { BoardView, type BoardData } from "./work/Board.js"
import { failureWords } from "./work/shared.js"
import { workWord } from "./work/words.js"
import { readAnswer, readFailure, readValue } from "../read-state.js"
import { requestPage } from "../overlays/index.js"
import { workPageHash, workRouteFromHash } from "../page-route.js"
import "./work/work.css"
import { WorkV2Page } from "./work/WorkV2.js"

/**
 * The new board (design-decisions T6, D30, D35; board-redesign §3.3): what is
 * happening now, and the Backlog beside it as a second tab.
 *
 * It is its own page, reached from its own drawer row, because where the new
 * board lives is not decided yet (U6): the Project Board — the 1:1 read-only
 * view of the Swift app's 787 cards — is left exactly as it is, and nothing
 * here reads `/v1/board`.
 *
 * Three kinds of thing, and one place for each (§3.1, #5):
 *   - the board: what a person needs to know now, as cards;
 *   - the Backlog: what is planned and not begun, as a list, on its own tab;
 *   - a session's to-dos: not here at all — on that session's page
 *     (session/Todos.tsx), folded.
 * Every command a person gives here is a POST to `/v1/work/*` under its own
 * Idempotency-Key, and the page reads the board again after it: what is shown
 * is always the daemon's answer, never this page's guess at one.
 */
type Tab = "board" | "backlog"

const EMPTY: BoardData = {
  board: { phase: "loading" }, proposals: null, decisions: null, proposalsTotal: 0, decisionsTotal: 0, digest: null, digestRead: false,
  unread: [],
}

/** How often a page on screen reads the board again: the sweep's own tick is 15 seconds. */
const REFRESH_MS = 30_000

function addressedWork() {
  return typeof location === "undefined"
    ? { project: "", fromProjects: false }
    : workRouteFromHash(location.hash)
}

/** Replace the page's scope without adding a false Back step. */
function replaceWorkAddress(project: string, fromProjects: boolean): void {
  const address = workPageHash(project, fromProjects ? "projects" : undefined)
  try {
    history.replaceState(history.state, "", address)
  } catch {
    location.hash = address
  }
}

function WorkPageView({ shown }: { shown: boolean }) {
  const T = L.strings
  const initialRoute = useRef(addressedWork())
  const [tab, setTab] = useState<Tab>("board")
  const [project, setProject] = useState(initialRoute.current.project)
  const [fromProjects, setFromProjects] = useState(initialRoute.current.fromProjects)
  const [data, setData] = useState<BoardData>(EMPTY)
  const [boardScope, setBoardScope] = useState<string | null>(null)
  const [backlog, setBacklog] = useState<BacklogPage | null>(null)
  const [backlogScope, setBacklogScope] = useState<string | null>(null)
  // Two things the status line says, kept apart: what the last reading found,
  // and what became of the last command. A reading that follows a refused
  // command must not wipe the refusal off the screen — "nothing was done"
  // and "done" would then look the same.
  const [status, setStatus] = useState<{ text: string; warn?: boolean } | null>(null)
  const [outcome, setOutcome] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  // These are deliberately two sources. Places are the machine's recognized
  // Project directory; workProjects keep work whose Project no longer (or
  // never did) appear there. Their union is the filter, never an intersection.
  const [places, setPlaces] = useState<ProjectPlace[] | null>(null)
  const [workProjects, setWorkProjects] = useState<string[]>([])
  const [seenProjects, setSeenProjects] = useState<string[]>([])
  const [workCatalogComplete, setWorkCatalogComplete] = useState(true)
  const [sourceFailures, setSourceFailures] = useState({ places: "", work: "" })
  const ticket = useRef(0)
  const sourceTicket = useRef(0)
  const title = useRef<HTMLHeadingElement>(null)

  const remember = useCallback((rows: Item[]) => {
    setSeenProjects((was) => {
      const next = new Set(was)
      for (const r of rows) if (r.project) next.add(r.project)
      return next.size === was.length ? was : [...next].sort()
    })
  }, [])

  const source = useMemo(() => {
    const byPath = new Map((places ?? []).map((place) => [place.path, place]))
    const recognized = new Set(byPath.keys())
    const namedByWork = new Set([...workProjects, ...seenProjects])
    const choices = new Set([...recognized, ...namedByWork])
    if (project) choices.add(project)
    return {
      byPath,
      choices: [...choices].sort((a, b) => a.localeCompare(b)),
      workOnly: [...namedByWork].filter((name) => !recognized.has(name)).sort(),
      placesOnly: [...recognized].filter((name) => !namedByWork.has(name)).sort(),
    }
  }, [places, project, seenProjects, workProjects])

  // Read both Project vocabularies independently. `/v1/places` is the real
  // machine directory (at most forty existing recent places). The work routes
  // have no Project-list endpoint, so their first unfiltered pages are the
  // comparison source and explicitly say when another page exists.
  const loadSources = useCallback(async () => {
    const mine = ++sourceTicket.current
    const [placePage, board, later, proposals] = await Promise.allSettled([
      readProjectPlaces(),
      readBoard(),
      readBacklog(),
      readProposals(),
    ])
    if (mine !== sourceTicket.current) return
    if (placePage.status === "fulfilled") setPlaces(placePage.value.places)
    const named = new Set<string>()
    if (board.status === "fulfilled") for (const row of board.value.rows) if (row.project) named.add(row.project)
    if (later.status === "fulfilled") for (const row of later.value.rows) if (row.project) named.add(row.project)
    if (proposals.status === "fulfilled") for (const row of proposals.value.rows) if (row.project) named.add(row.project)
    setWorkProjects([...named].sort())
    setWorkCatalogComplete(
      board.status === "fulfilled" && !board.value.next_cursor &&
      later.status === "fulfilled" && !later.value.next_cursor &&
      proposals.status === "fulfilled" && !proposals.value.next_cursor,
    )
    setSourceFailures({
      places: placePage.status === "rejected" ? failureWords(placePage.reason) : "",
      work: [
        board.status === "rejected" ? failureWords(board.reason) : "",
        later.status === "rejected" ? failureWords(later.reason) : "",
        proposals.status === "rejected" ? failureWords(proposals.reason) : "",
      ]
        .filter(Boolean)
        .join(" "),
    })
  }, [])

  // One reading of everything the tab shows. Each part fails on its own: an
  // unreadable digest leaves the board on screen, and says so (DG-7).
  const load = useCallback(async () => {
    const mine = ++ticket.current
    const p = project || undefined
    const scope = p ?? ""
    if (tab === "backlog") {
      try {
        const page = await readBacklog(p)
        if (mine !== ticket.current) return
        setBacklog(page)
        setBacklogScope(scope)
        remember(page.rows)
        setStatus(null)
      } catch (e) {
        if (mine !== ticket.current) return
        setBacklog(null)
        setBacklogScope(scope)
        setStatus({ text: workWord("unreadable") + " " + failureWords(e), warn: true })
      }
      return
    }
    const [board, proposals, decisions, digests] = await Promise.allSettled([
      readBoard(p),
      readProposals(p),
      readDecisions(),
      p ? Promise.resolve({ rows: [] }) : readDigests(),
    ])
    if (mine !== ticket.current) return
    // Only the board's refusal used to be read. The other three were dropped
    // where they were settled, so a refused "to confirm" drew as `?`, a
    // refused "to decide" drew as the empty section — which is the same
    // picture as "nothing is waiting for you" — and a refused digest drew as
    // a digest nobody had written. Each one now keeps its own sentence, said
    // by the catalog that names the code (DG-7: a read that did not happen is
    // never drawn as a read that found nothing).
    const next: BoardData = {
      board: board.status === "fulfilled"
        ? readAnswer(
            board.value,
            board.value.rows.length === 0 && SECTIONS.every((section) => (board.value.counts[section] ?? 0) === 0),
          )
        : readFailure(board.reason),
      proposals: proposals.status === "fulfilled" ? proposals.value.rows : null,
      decisions: decisions.status === "fulfilled"
        ? decisions.value.rows.filter((decision) => !p || decision.project === p)
        : null,
      proposalsTotal: proposals.status === "fulfilled" ? (proposals.value.counts.pending ?? proposals.value.rows.length) : 0,
      decisionsTotal: decisions.status === "fulfilled"
        ? (p ? decisions.value.rows.filter((decision) => decision.project === p).length
          : (decisions.value.counts.open ?? decisions.value.rows.length))
        : 0,
      digest: digests.status === "fulfilled" ? (digests.value.rows[0] ?? null) : null,
      digestRead: !!p || digests.status === "fulfilled",
      unread: [
        proposals.status === "rejected" ? failureWords(proposals.reason) : "",
        decisions.status === "rejected" ? failureWords(decisions.reason) : "",
        !p && digests.status === "rejected" ? failureWords(digests.reason) : "",
      ].filter(Boolean),
    }
    setData(next)
    setBoardScope(scope)
    const boardPage = readValue(next.board)
    if (boardPage) remember(boardPage.rows)
    if (board.status === "rejected") setStatus({ text: workWord("unreadable") + " " + failureWords(board.reason), warn: true })
    else if (boardPage?.sweep.stalled) setStatus({ text: workWord("sweepStalled"), warn: true })
    else setStatus(null)
  }, [remember, tab, project])

  // Arriving, changing tab or filter, and every so often while on screen and
  // visible. A hidden page reads nothing.
  useEffect(() => {
    if (!shown) return
    void load()
    void loadSources()
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") void load()
    }, REFRESH_MS)
    return () => clearInterval(timer)
  }, [shown, load, loadSources])

  useLayoutEffect(() => {
    if (!shown) return
    const route = addressedWork()
    setProject(route.project)
    setFromProjects(route.fromProjects)
    title.current?.focus({ preventScroll: true })
  }, [shown])

  useEffect(() => {
    if (!shown) return
    const followAddress = () => {
      const route = addressedWork()
      setProject(route.project)
      setFromProjects(route.fromProjects)
    }
    window.addEventListener("hashchange", followAddress)
    return () => window.removeEventListener("hashchange", followAddress)
  }, [shown])

  // A person's command: one at a time, its failure said where it happened,
  // and the board read again whatever the answer.
  const run = (task: () => Promise<unknown>) => {
    if (busy) return
    setBusy(true)
    setOutcome(null)
    task()
      .then(
        () => setOutcome(null),
        (e) => setOutcome(failureWords(e)),
      )
      .finally(() => {
        setBusy(false)
        void load()
      })
  }

  const onCommand = (it: Item, c: Command) => command(it, c)

  const more = async () => {
    const currentBoard = boardScope === project ? data : EMPTY
    const currentBacklog = backlogScope === project ? backlog : null
    const cursor = tab === "board" ? readValue(currentBoard.board)?.next_cursor : currentBacklog?.next_cursor
    if (!cursor) return
    try {
      if (tab === "board") {
        const page = await readBoard(project || undefined, cursor)
        setData((was) => {
          const held = readValue(was.board)
          return held ? { ...was, board: { phase: "ready", value: { ...page, rows: [...held.rows, ...page.rows] } } } : was
        })
        remember(page.rows)
      } else {
        const page = await readBacklog(project || undefined, cursor)
        setBacklog((was) => (was ? { ...page, rows: [...was.rows, ...page.rows] } : page))
        remember(page.rows)
      }
    } catch (e) {
      setOutcome(failureWords(e))
    }
  }

  // A scope changes before its read can answer. Never place the preceding
  // Project's cards under the new scope sentence while that answer is pending.
  const currentData = boardScope === project ? data : EMPTY
  const currentBacklog = backlogScope === project ? backlog : null
  const pending = currentData.proposals === null ? null : currentData.proposalsTotal
  const selectedPlace = source.byPath.get(project)
  const openConfirm = () => {
    setTab("board")
    requestAnimationFrame(() => {
      const fold = document.getElementById("work-confirm") as HTMLDetailsElement | null
      if (!fold) return
      fold.open = true
      fold.scrollIntoView({ block: "start", behavior: "smooth" })
    })
  }

  return (
    <section
      id="work"
      className="page board-page work-page"
      data-page-view="work"
      data-tab={tab}
      hidden={!shown}
      aria-labelledby="work-title"
    >
      <header className="board-head">
        {fromProjects && (
          <button className="board-button" type="button" onClick={() => requestPage({ page: "projects" })}>
            {workWord("backProjects")}
          </button>
        )}
        <div className="work-tabs" role="tablist" aria-label={workWord("nav")}>
          <button className="board-button work-tab" id="work-tab-board" type="button" role="tab"
            aria-selected={tab === "board"} onClick={() => setTab("board")}>
            {workWord("tabBoard")}
          </button>
          <button className="board-button work-tab" id="work-tab-backlog" type="button" role="tab"
            aria-selected={tab === "backlog"} onClick={() => setTab("backlog")}>
            {workWord("tabBacklog")}
          </button>
        </div>
        <div className="work-head-tools">
          <button className="board-button" id="work-confirm-badge" type="button" onClick={openConfirm}>
            {workWord("toConfirm")}
            <span className="work-badge" data-zero={pending ? undefined : ""}>{pending ?? "?"}</span>
          </button>
          <button className="board-button" id="work-new" type="button" aria-expanded={creating}
            onClick={() => setCreating((c) => !c)}>
            {workWord("newItem")}
          </button>
          <button className="board-button" id="work-refresh" type="button" disabled={busy} onClick={() => {
            void load()
            void loadSources()
          }}>
            {T.webInfoRefresh}
          </button>
        </div>
      </header>
      <div className="board-intro">
        <p className="board-eyebrow">{workWord(tab === "board" ? "boardEyebrow" : "backlogEyebrow")}</p>
        <h1 id="work-title" tabIndex={-1} ref={title}>
          {workWord(tab === "board" ? "boardTitle" : "backlogTitle")}
        </h1>
        <p className="work-lede">{workWord(tab === "board" ? "boardLede" : "backlogLede")}</p>
        <p className="work-lede" id="work-scope">
          {project
            ? workWord("scopeProject", { project: selectedPlace?.label || project })
            : workWord("scopeAll")}
          {project && selectedPlace?.label && selectedPlace.label !== project
            ? " · " + workWord("scopePath", { path: project })
            : ""}
        </p>
        <p className="work-lede">{workWord("scopeSource")}</p>
      </div>
      <div className="work-wrap">
        <p className="work-status" id="work-status" role="status" aria-live="polite"
          data-tone={outcome || status?.warn ? "warn" : undefined}>
          {[outcome, status?.text].filter(Boolean).join(" ") || (busy ? T.webLoading : "")}
        </p>
        {sourceFailures.places && (
          <p className="work-note" role="alert">{workWord("projectDirectoryUnreadable")} {sourceFailures.places}</p>
        )}
        {sourceFailures.work && (
          <p className="work-note" role="alert">{workWord("workDirectoryUnreadable")} {sourceFailures.work}</p>
        )}
        {!workCatalogComplete && <p className="work-note">{workWord("workCatalogPartial")}</p>}
        {places !== null && source.workOnly.length > 0 && (
          <p className="work-note">{workWord("workOnlyProjects", { n: source.workOnly.length })}</p>
        )}
        {places !== null && source.placesOnly.length > 0 && (
          <p className="work-note">{workWord("placesOnlyProjects", { n: source.placesOnly.length })}</p>
        )}
        <select className="work-input" id="work-project" aria-label={workWord("newProject")} value={project}
          onChange={(ev) => {
            setProject(ev.target.value)
            replaceWorkAddress(ev.target.value, fromProjects)
          }}>
          <option value="">{T.webSnippetsEveryProject}</option>
          {source.choices.map((name) => {
            const place = source.byPath.get(name)
            const label = place?.label && place.label !== name ? `${place.label} — ${name}` : name
            return (
              <option key={name} value={name}>
                {label}{places !== null && source.workOnly.includes(name) ? ` (${workWord("workOnlyOption")})` : ""}
              </option>
            )
          })}
        </select>
        {creating && (
          <NewItem
            place={tab === "backlog" ? "backlog" : "board"}
            projects={source.choices}
            project={project}
            busy={busy}
            onCancel={() => setCreating(false)}
            onCreate={(n) => {
              setCreating(false)
              if (n.place !== tab) setTab(n.place)
              run(() => createItem(n))
            }}
          />
        )}
        {tab === "board" ? (
          <BoardView
            data={currentData}
            scoped={!!project}
            busy={busy}
            run={run}
            onCommand={onCommand}
            onMore={() => void more()}
            onAnswerProposal={(p, a) => answerProposal(p.id, a)}
            onResolveProposal={(p, resolution, evidence) => resolveProposal(p.id, resolution, evidence)}
            onAnswerDecision={(d, o) => answerDecision(d.id, o)}
          />
        ) : (
          <BacklogView page={currentBacklog} busy={busy} run={run} onCommand={onCommand} onMore={() => void more()} />
        )}
      </div>
    </section>
  )
}

/** A person's new item: a title, its project, and whether it starts now or later. */
function NewItem({
  place,
  projects,
  project,
  busy,
  onCancel,
  onCreate,
}: {
  place: Tab
  projects: string[]
  project: string
  busy: boolean
  onCancel: () => void
  onCreate: (n: { title: string; project: string; place: Tab }) => void
}) {
  const [title, setTitle] = useState("")
  const [where, setWhere] = useState(project)
  const [to, setTo] = useState<Tab>(place)
  const ready = title.trim() !== "" && where.trim() !== ""
  return (
    <form
      className="work-new"
      id="work-new-form"
      onSubmit={(ev) => {
        ev.preventDefault()
        if (ready) onCreate({ title: title.trim(), project: where.trim(), place: to })
      }}
    >
      <div className="work-new-row">
        <input className="work-input" name="title" aria-label={workWord("newTitle")} placeholder={workWord("newTitle")}
          value={title} maxLength={200} autoFocus onChange={(ev) => setTitle(ev.target.value)} />
        <input className="work-input" name="project" aria-label={workWord("newProject")} placeholder={workWord("newProject")}
          list="work-projects" value={where} onChange={(ev) => setWhere(ev.target.value)} />
        <datalist id="work-projects">
          {projects.map((p) => (
            <option key={p} value={p} />
          ))}
        </datalist>
      </div>
      <div className="work-new-row">
        <label>
          <input type="radio" name="place" value="board" checked={to === "board"} onChange={() => setTo("board")} />
          {workWord("newToBoard")}
        </label>
        <label>
          <input type="radio" name="place" value="backlog" checked={to === "backlog"} onChange={() => setTo("backlog")} />
          {workWord("newToBacklog")}
        </label>
      </div>
      <div className="work-actions">
        <button className="chip on" type="submit" disabled={busy || !ready}>
          {workWord("create")}
        </button>
        <button className="chip" type="button" onClick={onCancel}>
          {L.strings.webCancel}
        </button>
      </div>
    </form>
  )
}

export const page: PageModule = { id: "work", Component: WorkV2Page }
