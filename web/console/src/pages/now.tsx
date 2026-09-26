import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import type { BrokerPendingLanding, TaskList, TaskRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import { requestPage } from "../overlays/index.js"
import {
  liveTasks,
  readDecisions,
  readLandings,
  readProposals,
  readTasks,
  type DecisionPage,
  type ProposalPage,
} from "./now/api.js"
import { ageWords, draw, readClock, type Drawn, type Source } from "./now/freshness.js"
import { failureWords } from "./now/shared.js"
import { joinWaiting, type Block, type WaitingRow } from "./now/waiting.js"
import { nowWord } from "./now/words.js"
import "./now/now.css"

/**
 * "Where things stand" (work-system-review §5.2, W4).
 *
 * The person asked three times in one day what the state of things was, and
 * the answer was spread across four places, two of which had no screen at
 * all: the board had every finished row, "to confirm" had five, the landing
 * ledger had nineteen and nothing drew it, and three tasks were running and
 * nothing drew those either. This page is those three questions, side by
 * side, **derived from routes this daemon already answers and storing
 * nothing**.
 *
 * Three rules it is built on, in the order they matter:
 *
 *  1. **A source that could not be read is never drawn as `0`.** That is the
 *     one failure this page exists to stop, and it is not a styling choice:
 *     `now/freshness.ts` decides what a block may print, and a reading that
 *     did not happen has no count at any freshness. The daemon grew a fourth
 *     word for it — `unverified`, read in full and possibly already out of
 *     date — because until it existed a ledger that could not say whether its
 *     rows had landed answered `current`, which every screen drew as settled.
 *  2. **Each block loads and fails alone.** Four calls, not one: the page is
 *     read on a phone over a relay, and one aggregate answer would make one
 *     unreachable source a blank page. There is no `Promise.all` here on
 *     purpose.
 *  3. **A named refusal keeps its name** (`refusals/scan.ts`). The block says
 *     the sentence the catalog has for that code and the `code · ref` after
 *     it, because a person on a phone can act on "the broker store could not
 *     be opened" and can only shrug at "something went wrong".
 *
 * What it deliberately does not do is let a person change anything. Every
 * block ends in a way through to the page that owns the thing — the board for
 * a proposal, a session for a task — so that this page stays one answer to
 * one question and does not quietly become a second board (CM-2).
 */

/** How often a page on screen reads again: the board's own interval. */
const REFRESH_MS = 30_000

/** How many rows a block lists before it says how many more there are. */
const ROWS_SHOWN = 4

const NOTHING: Block<never> = { reading: { read: false }, rows: [] }

function NowPageView({ shown }: { shown: boolean }) {
  const T = L.strings
  const [doing, setDoing] = useState<Block<TaskRow>>(NOTHING)
  const [owed, setOwed] = useState<Block<BrokerPendingLanding>>(NOTHING)
  const [waiting, setWaiting] = useState<Block<WaitingRow>>(NOTHING)
  const [busy, setBusy] = useState(false)
  const ticket = useRef(0)
  const title = useRef<HTMLHeadingElement>(null)

  // Three readings, each its own request and its own state. A block that
  // fails leaves the other two on screen, which is the whole reason this is
  // not one call.
  const load = useCallback(async () => {
    const mine = ++ticket.current
    const fresh = () => mine === ticket.current
    setBusy(true)

    const tasks = readTasks().then(
      (list: TaskList) => {
        if (!fresh()) return
        const rows = liveTasks(list)
        setDoing({ reading: { read: true, rows: rows.length, source: list.source as Source }, rows })
      },
      (e) => {
        if (!fresh()) return
        setDoing({ reading: { read: false, failure: failureWords(e) }, rows: [] })
      },
    )

    const landings = readLandings().then(
      (list) => {
        if (!fresh()) return
        const rows = [...list.landings].sort((a, b) => b.age_seconds - a.age_seconds)
        setOwed({
          reading: { read: true, rows: rows.length, source: list.sources.landings as Source },
          rows,
        })
      },
      (e) => {
        if (!fresh()) return
        setOwed({ reading: { read: false, failure: failureWords(e) }, rows: [] })
      },
    )

    // Two routes behind one block, and they are not merged into one reading:
    // if only the decisions answer, the block shows the proposals it has and
    // says the other half did not arrive. The freshness it prints is the
    // worse of the two, because a block is only as good as its weakest source.
    const waits = Promise.all([
      readProposals().then(
        (p: ProposalPage) => ({ ok: true as const, page: p }),
        (e: unknown) => ({ ok: false as const, why: failureWords(e) }),
      ),
      readDecisions().then(
        (d: DecisionPage) => ({ ok: true as const, page: d }),
        (e: unknown) => ({ ok: false as const, why: failureWords(e) }),
      ),
    ]).then(([proposals, decisions]) => {
      if (!fresh()) return
      setWaiting(joinWaiting(proposals, decisions))
    })

    await Promise.all([tasks, landings, waits])
    if (fresh()) setBusy(false)
  }, [])

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

  const doingDrawn = draw("doing", doing.reading)
  const owedDrawn = draw("owed", owed.reading)
  const waitingDrawn = draw("waiting", waiting.reading)

  return (
    <section
      id="now"
      className="page board-page now-page"
      data-page-view="now"
      hidden={!shown}
      aria-labelledby="now-title"
    >
      <header className="board-head">
        <div className="work-head-tools">
          <button className="board-button" id="now-refresh" type="button" disabled={busy} onClick={() => void load()}>
            {busy ? T.webLoading : nowWord("refresh")}
          </button>
        </div>
      </header>
      <div className="board-intro">
        <p className="board-eyebrow">{nowWord("eyebrow")}</p>
        <h1 id="now-title" tabIndex={-1} ref={title}>
          {nowWord("title")}
        </h1>
        <p className="now-lede">{nowWord("lede")}</p>
      </div>
      <div className="now-wrap">
        <div className="now-blocks">
          <BlockView drawn={doingDrawn} source={doing.reading.source} title={nowWord("doingTitle")}
            lede={nowWord("doingLede")} empty={nowWord("doingNone")} rows={doing.rows.length}>
            <ul className="now-rows">
              {doing.rows.slice(0, ROWS_SHOWN).map((row) => (
                <li key={row.id}>
                  <b>{row.title || row.id}</b>
                  <div className="now-meta">
                    <span>{nowWord("doingFor", { age: ageWords(ageOf(row)) })}</span>
                    <span>{row.root?.label ? nowWord("doingRoot", { root: row.root.label }) : nowWord("doingNoRoot")}</span>
                  </div>
                </li>
              ))}
            </ul>
            <More shown={ROWS_SHOWN} total={doing.rows.length} />
          </BlockView>

          <BlockView drawn={owedDrawn} source={owed.reading.source} title={nowWord("owedTitle")}
            lede={nowWord("owedLede")} empty={nowWord("owedNone")} rows={owed.rows.length}>
            <ul className="now-rows">
              {owed.rows.slice(0, ROWS_SHOWN).map((row) => (
                <li key={row.id}>
                  <b>{row.title || row.id}</b>
                  <div className="now-meta">
                    <span>{nowWord("owedOldest", { age: ageWords(row.age_seconds) })}</span>
                    <span>{nowWord("owedWho", {
                      who: row.ownership.subject === "executor" ? nowWord("owedWhoExecutor") : nowWord("owedWhoRoot"),
                    })}</span>
                  </div>
                  <div className="now-note">{settlementWords(row)}</div>
                  <div className="now-note">
                    {row.target ? nowWord("owedTarget", { target: row.target }) : nowWord("owedNoTarget")}
                  </div>
                </li>
              ))}
            </ul>
            <More shown={ROWS_SHOWN} total={owed.rows.length} />
          </BlockView>

          <BlockView drawn={waitingDrawn} source={waiting.reading.source} title={nowWord("waitingTitle")}
            lede={nowWord("waitingLede")} empty={nowWord("waitingNone")} rows={waiting.rows.length}>
            <ul className="now-rows">
              {waiting.rows.slice(0, ROWS_SHOWN).map((row) => (
                <li key={row.kind + ":" + row.id}>
                  <b>{row.title}</b>
                  <div className="now-meta">
                    <span>{nowWord("waitingOldest", { age: ageWords(row.age) })}</span>
                  </div>
                  {/* A proposal is accepted or rejected in the Board's Agent
                      proposals queue (work system v2 §10); this page only
                      points there. */}
                  {row.kind === "proposal" && (
                    <button className="now-go" type="button" onClick={() => requestPage({ page: "work" })}>
                      {nowWord("waitingOnBoard")}
                    </button>
                  )}
                </li>
              ))}
            </ul>
            <More shown={ROWS_SHOWN} total={waiting.rows.length} />
            <div className="now-actions">
              <button className="board-button" type="button" onClick={() => requestPage({ page: "work" })}>
                {nowWord("waitingGo")}
              </button>
            </div>
          </BlockView>
        </div>
      </div>
    </section>
  )
}

/**
 * One block: its count, the sentence that qualifies the count, and its rows.
 *
 * `freshness.draw` decided all of it and answered in word keys; this turns
 * those into the catalog's sentences and nothing else. The count is
 * `drawn.count === null` or a number — there is no branch here that could put
 * a `0` where the rule said there was no number.
 */
function BlockView({
  drawn,
  source,
  title,
  lede,
  empty,
  rows,
  children,
}: {
  drawn: Drawn
  source: Source | undefined
  title: string
  lede: string
  empty: string
  rows: number
  children: ReactNode
}) {
  const clock = readClock(source)
  const line = drawn.said ??
    (drawn.line ? nowWord(drawn.line, drawn.why ? { why: nowWord(drawn.why) } : {}) : nowWord("unreadable"))
  return (
    <section className="now-block" data-tone={drawn.tone}>
      <div className="now-head">
        <h2>{title}</h2>
        <span className="now-count">{drawn.count === null ? nowWord("unknownCount") : drawn.count}</span>
      </div>
      <p className="now-block-lede">{lede}</p>
      {/* The number is never on screen without this line. */}
      <p className="now-fresh" role="status">
        {line}
        {clock && <span className="now-fresh-at">{nowWord("freshAt", { when: clock })}</span>}
      </p>
      {/* "Nothing here" is said only by a reading that happened and found
          nothing. A block with no count shows no rows and no empty line
          either: both would be claims about what is there. */}
      {drawn.count === null ? null : rows ? children : <p className="now-empty">{empty}</p>}
    </section>
  )
}

function More({ shown, total }: { shown: number; total: number }) {
  if (total <= shown) return null
  return <p className="now-more">{nowWord("more", { n: total - shown })}</p>
}

/** How long a task has been going, from whichever time the row carries. */
function ageOf(row: TaskRow): number {
  const started = row.briefedAt || row.spawnedAt || row.created || row.created_at || 0
  if (!started) return 0
  return Math.max(0, Math.floor(Date.now() / 1000) - started)
}

/**
 * What the delivery branch held when the task ended (W1-a).
 *
 * An absent settlement is not `branch_empty`. It is a task that had no branch
 * of its own — it wrote the shared checkout — or a record written before this
 * was kept, and saying "its branch carried nothing" about either would be an
 * invention.
 */
function settlementWords(row: BrokerPendingLanding): string {
  switch (row.settlement) {
    case "branch_empty":
      return nowWord("owedBranchEmpty")
    case "branch_carries_commits":
      return nowWord("owedBranchCarries")
    case "branch_unreadable":
      return nowWord("owedBranchUnreadable")
  }
  return nowWord("owedBranchUnknown")
}

export const page: PageModule = { id: "now", Component: NowPageView }
