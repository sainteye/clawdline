import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import {
  answerDecision,
  answerProposal,
  command,
  createItem,
  readBacklog,
  readBoard,
  readDecisions,
  readDigests,
  readProposals,
  type BacklogPage,
  type Command,
  type Item,
} from "./work/api.js"
import { BacklogView } from "./work/Backlog.js"
import { BoardView, type BoardData } from "./work/Board.js"
import { failureWords } from "./work/shared.js"
import { workWord } from "./work/words.js"
import "./work/work.css"

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
  board: null, proposals: null, decisions: null, proposalsTotal: 0, decisionsTotal: 0, digest: null, digestRead: false,
}

/** How often a page on screen reads the board again: the sweep's own tick is 15 seconds. */
const REFRESH_MS = 30_000

function WorkPageView({ shown }: { shown: boolean }) {
  const T = L.strings
  const [tab, setTab] = useState<Tab>("board")
  const [project, setProject] = useState("")
  const [data, setData] = useState<BoardData>(EMPTY)
  const [backlog, setBacklog] = useState<BacklogPage | null>(null)
  // Two things the status line says, kept apart: what the last reading found,
  // and what became of the last command. A reading that follows a refused
  // command must not wipe the refusal off the screen — "nothing was done"
  // and "done" would then look the same.
  const [status, setStatus] = useState<{ text: string; warn?: boolean } | null>(null)
  const [outcome, setOutcome] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  // Every project this page has seen an item in, for the filter and the new
  // item's field; a filter never shrinks its own choices.
  const [projects, setProjects] = useState<string[]>([])
  const ticket = useRef(0)
  const title = useRef<HTMLHeadingElement>(null)

  const remember = (rows: Item[]) => {
    setProjects((was) => {
      const next = new Set(was)
      for (const r of rows) if (r.project) next.add(r.project)
      return next.size === was.length ? was : [...next].sort()
    })
  }

  // One reading of everything the tab shows. Each part fails on its own: an
  // unreadable digest leaves the board on screen, and says so (DG-7).
  const load = useCallback(async () => {
    const mine = ++ticket.current
    const p = project || undefined
    if (tab === "backlog") {
      try {
        const page = await readBacklog(p)
        if (mine !== ticket.current) return
        setBacklog(page)
        remember(page.rows)
        setStatus(null)
      } catch (e) {
        if (mine !== ticket.current) return
        setStatus({ text: workWord("unreadable") + " " + failureWords(e), warn: true })
      }
      return
    }
    const [board, proposals, decisions, digests] = await Promise.allSettled([
      readBoard(p),
      readProposals(p),
      readDecisions(),
      readDigests(),
    ])
    if (mine !== ticket.current) return
    const next: BoardData = {
      board: board.status === "fulfilled" ? board.value : null,
      proposals: proposals.status === "fulfilled" ? proposals.value.rows : null,
      decisions: decisions.status === "fulfilled" ? decisions.value.rows : null,
      proposalsTotal: proposals.status === "fulfilled" ? (proposals.value.counts.pending ?? proposals.value.rows.length) : 0,
      decisionsTotal: decisions.status === "fulfilled" ? (decisions.value.counts.open ?? decisions.value.rows.length) : 0,
      digest: digests.status === "fulfilled" ? (digests.value.rows[0] ?? null) : null,
      digestRead: digests.status === "fulfilled",
    }
    setData(next)
    if (next.board) remember(next.board.rows)
    if (board.status === "rejected") setStatus({ text: workWord("unreadable") + " " + failureWords(board.reason), warn: true })
    else if (next.board?.sweep.stalled) setStatus({ text: workWord("sweepStalled"), warn: true })
    else setStatus(null)
  }, [tab, project])

  // Arriving, changing tab or filter, and every so often while on screen and
  // visible. A hidden page reads nothing.
  useEffect(() => {
    if (!shown) return
    void load()
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") void load()
    }, REFRESH_MS)
    return () => clearInterval(timer)
  }, [shown, load])

  useLayoutEffect(() => {
    if (shown) title.current?.focus({ preventScroll: true })
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
    const cursor = tab === "board" ? data.board?.next_cursor : backlog?.next_cursor
    if (!cursor) return
    try {
      if (tab === "board") {
        const page = await readBoard(project || undefined, cursor)
        setData((was) => (was.board ? { ...was, board: { ...page, rows: [...was.board.rows, ...page.rows] } } : was))
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

  const pending = data.proposals === null ? null : data.proposalsTotal
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
          <button className="board-button" id="work-refresh" type="button" disabled={busy} onClick={() => void load()}>
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
      </div>
      <div className="work-wrap">
        <p className="work-status" id="work-status" role="status" aria-live="polite"
          data-tone={outcome || status?.warn ? "warn" : undefined}>
          {[outcome, status?.text].filter(Boolean).join(" ") || (busy ? T.webLoading : "")}
        </p>
        {projects.length > 1 && (
          <select className="work-input" id="work-project" aria-label={workWord("newProject")} value={project}
            onChange={(ev) => setProject(ev.target.value)}>
            <option value="">{T.webSnippetsEveryProject}</option>
            {projects.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        )}
        {creating && (
          <NewItem
            place={tab === "backlog" ? "backlog" : "board"}
            projects={projects}
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
            data={data}
            busy={busy}
            run={run}
            onCommand={onCommand}
            onMore={() => void more()}
            onAnswerProposal={(p, a) => answerProposal(p.id, a)}
            onAnswerDecision={(d, o) => answerDecision(d.id, o)}
          />
        ) : (
          <BacklogView page={backlog} busy={busy} run={run} onCommand={onCommand} onMore={() => void more()} />
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

export const page: PageModule = { id: "work", Component: WorkPageView }
