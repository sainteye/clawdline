import { useEffect, useRef, useState } from "react"
import * as L from "../../legacy/bridge.js"
import {
  SECTIONS,
  type BoardPage,
  type Command,
  type Decision,
  type Digest,
  type Item,
  type Proposal,
  type Section,
} from "./api.js"
import { openTimeline } from "../../legacy/timeline-bridge.js"
import { requestPage } from "../../overlays/index.js"
import { proposalFoldShouldOpen } from "./fold.js"
import { ownerWords, reasonWords, taskWords, when } from "./shared.js"
import { workWord, type WorkWord } from "./words.js"
import { nextWord } from "../../next-strings.js"
import { readAnswered, type ReadState } from "../../read-state.js"

/** A command a card asks the page to carry out; the page reads the board again after it. */
export type Run = (task: () => Promise<unknown>) => void

export interface BoardData {
  board: ReadState<BoardPage>
  proposals: Proposal[] | null
  decisions: Decision[] | null
  // The daemon's totals: a list is one page, and what did not fit is said.
  proposalsTotal: number
  decisionsTotal: number
  digest: Digest | null
  digestRead: boolean
  /** One sentence per part of this page whose read was refused; empty when all answered. */
  unread: string[]
}

const SECTION_WORD: Record<Section, WorkWord> = {
  decide: "sectionDecide",
  active: "sectionActive",
  scheduled: "sectionScheduled",
  done: "sectionDone",
}

/**
 * The board tab (board-redesign §3.3), top to bottom: the daily digest and
 * the "to confirm" area, folded; then Waiting on you, In progress, Scheduled
 * this week, and Recently done, folded. Only `board_items` are here — a
 * session's to-dos never are.
 */
export function BoardView({
  data,
  busy,
  run,
  onCommand,
  onMore,
  onAnswerProposal,
  onResolveProposal,
  onAnswerDecision,
}: {
  data: BoardData
  busy: boolean
  run: Run
  onCommand: (it: Item, c: Command) => Promise<unknown>
  onMore: () => void
  onAnswerProposal: (p: Proposal, a: "track" | "later" | "no") => Promise<unknown>
  onResolveProposal: (p: Proposal, resolution: string, evidence: string) => Promise<unknown>
  onAnswerDecision: (d: Decision, option: string) => Promise<unknown>
}) {
  const board = data.board.phase === "ready" || data.board.phase === "empty_authoritative" ? data.board.value : null
  const boardAnswered = readAnswered(data.board)
  const bySection: Record<Section, Item[]> = { decide: [], active: [], scheduled: [], done: [] }
  for (const it of board?.rows ?? []) if (it.section) bySection[it.section].push(it)
  const shown = board?.rows.length ?? 0
  const total = board ? SECTIONS.reduce((n, s) => n + (board.counts[s] ?? 0), 0) : 0
  const decisions = data.decisions ?? []

  return (
    <>
      {data.unread.map((said) => (
        <p key={said} className="work-note" role="alert">
          {said}
        </p>
      ))}
      <DigestFold digest={data.digest} read={data.digestRead} />
      <ProposalsFold proposals={data.proposals} total={data.proposalsTotal} busy={busy} run={run}
        onAnswer={onAnswerProposal} onResolve={onResolveProposal} />
      {data.board.phase === "loading" && (
        <p className="work-note" role="status">{L.strings.webLoading}</p>
      )}
      {boardAnswered && SECTIONS.map((section) => {
        const rows = bySection[section]
        const counted = board ? board.counts[section] : 0
        const count = (counted ?? 0) + (section === "decide" ? data.decisionsTotal : 0)
        const head = (
          <div className="work-section-head">
            <h2>{workWord(SECTION_WORD[section])}</h2>
            <span className="work-count">{count}</span>
          </div>
        )
        const body = (
          <>
            {section === "decide" && <p className="work-lede work-sub">{workWord("decideLede")}</p>}
            {rows.length === 0 && !(section === "decide" && decisions.length) ? (
              <p className="work-empty">{workWord("sectionEmpty")}</p>
            ) : (
              <div className="work-cards">
                {section === "decide" &&
                  decisions.map((d) => (
                    <DecisionCard key={d.id} decision={d} busy={busy} run={run} onAnswer={onAnswerDecision} />
                  ))}
                {rows.map((it) => (
                  <ItemCard key={it.id} item={it} section={section} busy={busy} run={run} onCommand={onCommand} />
                ))}
              </div>
            )}
            {section === "decide" && data.decisionsTotal > decisions.length && (
              <p className="work-note">{workWord("more", { n: data.decisionsTotal - decisions.length })}</p>
            )}
          </>
        )
        if (section === "done") {
          return (
            <details key={section} className="work-section work-done" data-section={section}>
              <summary>{head}</summary>
              {body}
            </details>
          )
        }
        return (
          <section key={section} className="work-section" data-section={section} aria-label={workWord(SECTION_WORD[section])}>
            {head}
            {body}
          </section>
        )
      })}
      {board?.next_cursor && (
        <button className="board-button work-more" type="button" disabled={busy} onClick={onMore}>
          {workWord("more", { n: Math.max(0, total - shown) })}
        </button>
      )}
    </>
  )
}

/** What a card may be asked to do, by where it is (work.Decide's rules). */
function opsFor(section: Section): { op: Command["op"]; word: WorkWord; tone?: "on" | "danger" }[] {
  switch (section) {
    case "decide":
      return [
        { op: "accept", word: "opAccept", tone: "on" },
        { op: "rework", word: "opRework" },
        { op: "defer", word: "opDefer" },
        { op: "drop", word: "opDrop", tone: "danger" },
      ]
    case "active":
      return [
        { op: "defer", word: "opDefer" },
        { op: "handover", word: "opHandover" },
        { op: "done_elsewhere", word: "opDoneElsewhere" },
        { op: "untrack", word: "opUntrack" },
        { op: "drop", word: "opDrop", tone: "danger" },
      ]
    case "scheduled":
      return [
        { op: "defer", word: "opDefer" },
        { op: "done_elsewhere", word: "opDoneElsewhere" },
        { op: "handover", word: "opHandover" },
        { op: "drop", word: "opDrop", tone: "danger" },
      ]
    case "done":
      return []
  }
}

function ItemCard({
  item,
  section,
  busy,
  run,
  onCommand,
}: {
  item: Item
  section: Section
  busy: boolean
  run: Run
  onCommand: (it: Item, c: Command) => Promise<unknown>
}) {
  // One question at a time on a card: who to hand it to, why no delivery
  // named this item, or "really drop it?".
  const [asking, setAsking] = useState<"handover" | "done_elsewhere" | "drop" | null>(null)
  const [owner, setOwner] = useState("")
  const [why, setWhy] = useState("")
  const d = item.derived
  return (
    <article className="work-card" data-work-id={item.id} data-state={d.state}>
      <span className="work-state">{reasonWords(item)}</span>
      <h3>{item.title}</h3>
      <div className="work-meta">
        <span>{item.project}</span>
        {item.owner && <span>{ownerWords(item.owner)}</span>}
        {(d.tasks.total > 0 || item.unknown_tasks > 0) && <span>{taskWords(item)}</span>}
        {item.start_on && <span>{workWord("startOn", { date: item.start_on })}</span>}
      </div>
      {/* The history of this one thing (work-system-review §5.2, W6). The
          Timeline used to be reached only from the old Project Board, which
          draws nothing on this machine, so a live page hung off a dead one.
          It belongs here: a timeline is one Project's, and this card is the
          thing whose history a reader wants. The Board's own tab is left as
          it was — this is a second door, not a replacement. */}
      {item.project && (
        <div className="work-actions">
          <button
            className="chip"
            type="button"
            data-timeline-for={item.id}
            onClick={() => {
              openTimeline(item.project, "work")
              requestPage({ page: "timeline" })
            }}
          >
            {workWord("timeline")}
          </button>
        </div>
      )}
      {d.last_evidence_at && <div className="work-clock">{workWord("lastEvidence", { when: when(d.last_evidence_at) })}</div>}
      {d.stall_at && <div className="work-clock">{workWord("stallClock", { when: when(d.stall_at) })}</div>}
      {d.closure_due_at && <div className="work-clock">{workWord("closureClock", { when: when(d.closure_due_at) })}</div>}
      {asking === "handover" ? (
        <form
          className="work-actions"
          onSubmit={(ev) => {
            ev.preventDefault()
            const to = owner.trim()
            if (!to) return
            setAsking(null)
            run(() => onCommand(item, { op: "handover", owner: to }))
          }}
        >
          <input
            className="work-input"
            aria-label={workWord("handoverTo")}
            placeholder={workWord("handoverTo")}
            value={owner}
            autoFocus
            onChange={(ev) => setOwner(ev.target.value)}
          />
          <button className="chip on" type="submit" disabled={busy || !owner.trim()}>
            {workWord("opHandover")}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </form>
      ) : asking === "done_elsewhere" ? (
        <form
          className="work-actions"
          onSubmit={(ev) => {
            ev.preventDefault()
            const said = why.trim()
            if (!said) return
            setAsking(null)
            run(() => onCommand(item, { op: "done_elsewhere", reason: said }))
          }}
        >
          <input
            className="work-input"
            aria-label={workWord("doneElsewhereWhy")}
            placeholder={workWord("doneElsewhereWhy")}
            value={why}
            autoFocus
            onChange={(ev) => setWhy(ev.target.value)}
          />
          <button className="chip on" type="submit" disabled={busy || !why.trim()}>
            {workWord("opDoneElsewhere")}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </form>
      ) : asking === "drop" ? (
        <div className="work-actions" role="group">
          <button
            className="chip danger"
            type="button"
            disabled={busy}
            autoFocus
            onClick={() => {
              setAsking(null)
              run(() => onCommand(item, { op: "drop" }))
            }}
          >
            {workWord("opDrop")} — {item.title}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </div>
      ) : (
        opsFor(section).length > 0 && (
          <div className="work-actions">
            {opsFor(section).map(({ op, word, tone }) => (
              <button
                key={op}
                className={tone ? `chip ${tone}` : "chip"}
                type="button"
                data-op={op}
                disabled={busy}
                onClick={() => {
                  if (op === "handover" || op === "drop" || op === "done_elsewhere") setAsking(op)
                  else run(() => onCommand(item, { op }))
                }}
              >
                {workWord(word)}
              </button>
            ))}
          </div>
        )
      )}
    </article>
  )
}

/** A question a session asked a person (Waiting on you): its options, and what stands if nobody answers. */
function DecisionCard({
  decision,
  busy,
  run,
  onAnswer,
}: {
  decision: Decision
  busy: boolean
  run: Run
  onAnswer: (d: Decision, option: string) => Promise<unknown>
}) {
  const fallback = decision.options.find((o) => o.id === decision.default)?.label ?? decision.default
  return (
    <article className="work-card" data-decision-id={decision.id}>
      <span className="work-state">{decision.blocking ? workWord("decisionBlocking") : workWord("sectionDecide")}</span>
      <h3>{decision.question}</h3>
      <div className="work-meta">
        {decision.project && <span>{decision.project}</span>}
        <span>{workWord("decisionFrom", { session: decision.session_id })}</span>
      </div>
      <div className="work-clock">{workWord("decisionDefault", { when: when(decision.due_at), option: fallback })}</div>
      <div className="work-actions">
        {decision.options.map((o) => (
          <button
            key={o.id}
            className={o.id === decision.default ? "chip on" : "chip"}
            type="button"
            data-option={o.id}
            disabled={busy}
            onClick={() => run(() => onAnswer(decision, o.id))}
          >
            {o.label}
          </button>
        ))}
      </div>
    </article>
  )
}

const SIGNAL_WORD: Record<string, WorkWord> = {
  cross_session: "signalCrossSession",
  long_lived: "signalLongLived",
  external_effect: "signalExternalEffect",
  leftover: "signalLeftover",
}

/**
 * The "to confirm" area: proposals nobody has answered (board-redesign §4.3).
 *
 * Twenty-six of them looked alike on 2026-09-20 and the person could not tell
 * which were worth reading, so two things are said here that the rows carried
 * and did not show:
 *
 *   - what the question is about. A row's task id is the subject for a line of
 *     work and is only provenance for a leftover (PT-9), and a card that
 *     printed neither made a finished delivery look like an unanswered
 *     question;
 *   - when it goes away by itself. Every row expires, and none of them said so.
 *
 * Ordering is by what can still change the answer, not by when it arrived: an
 * effect outside this machine first, then a line owed past a day, then the
 * leftovers nobody has picked up, then the rest — and inside each, whatever
 * runs out of time first.
 */
const GROUPS = [
  { word: "proposalGroupEffect", has: (p: Proposal) => p.signals.includes("external_effect") },
  { word: "proposalGroupStuck", has: (p: Proposal) => p.signals.includes("long_lived") },
  { word: "proposalGroupLeftover", has: (p: Proposal) => p.signals.includes("leftover") },
  { word: "proposalGroupOther", has: () => true },
] as const

type GroupWord = (typeof GROUPS)[number]["word"]

function grouped(rows: Proposal[]): { word: GroupWord; rows: Proposal[] }[] {
  const out: { word: GroupWord; rows: Proposal[] }[] = []
  const taken = new Set<string>()
  for (const g of GROUPS) {
    const mine = rows.filter((p) => !taken.has(p.id) && g.has(p))
    for (const p of mine) taken.add(p.id)
    if (mine.length) {
      out.push({ word: g.word, rows: mine.sort((a, b) => a.expires_at - b.expires_at) })
    }
  }
  return out
}

/** Whole days left before it leaves on its own; never negative. */
function daysLeft(p: Proposal, now: number): number {
  return Math.max(0, Math.floor((p.expires_at - now) / 86_400))
}

function ProposalsFold({
  proposals,
  total,
  busy,
  run,
  onAnswer,
  onResolve,
}: {
  proposals: Proposal[] | null
  total: number
  busy: boolean
  run: Run
  onAnswer: (p: Proposal, a: "track" | "later" | "no") => Promise<unknown>
  onResolve: (p: Proposal, resolution: string, evidence: string) => Promise<unknown>
}) {
  const rows = proposals ?? []
  const now = Date.now() / 1000
  const groups = grouped(rows)
  const [open, setOpen] = useState(false)
  const [resolving, setResolving] = useState<string | null>(null)
  const [resolution, setResolution] = useState("")
  const [evidence, setEvidence] = useState("")
  const previousTotal = useRef<number | null>(null)

  // The Now page's action opens this page. When proposals have arrived,
  // their answer controls must therefore be visible without a second,
  // undiscoverable press on a folded summary. A person may still close the
  // fold; the next 30-second refresh does not force it open again unless the
  // pending total actually changes.
  useEffect(() => {
    if (proposalFoldShouldOpen(previousTotal.current, total)) setOpen(true)
    previousTotal.current = total
  }, [total])

  return (
    <details className="work-fold" id="work-confirm" open={open} onToggle={(ev) => setOpen(ev.currentTarget.open)}>
      <summary>
        <strong>{workWord("toConfirm")}</strong>
        <span className="work-badge" data-zero={total === 0 ? "" : undefined}>
          {proposals === null ? "?" : total}
        </span>
      </summary>
      <div className="work-fold-body">
        <p>{workWord("toConfirmLede")}</p>
        {proposals === null ? (
          <p className="work-note">{workWord("unreadable")}</p>
        ) : rows.length === 0 ? (
          <p>{workWord("toConfirmNone")}</p>
        ) : (
          groups.map((g) => (
            <div key={g.word} className="work-group" data-proposal-group={g.word}>
              {groups.length > 1 && <p className="work-note">{workWord(g.word)}</p>}
              <ul className="work-lines">
                {g.rows.map((p) => {
                  const left = daysLeft(p, now)
                  return (
                    <li key={p.id} data-proposal-id={p.id}>
                      <b>{p.title}</b>
                      <span className="work-sub">
                        {p.signals.includes("leftover")
                          ? workWord("proposalKindLeftover", { task: (p.task_id ?? "").slice(0, 8) })
                          : workWord("proposalKindLine")}
                      </span>
                      <span className="work-sub">
                        {p.project} ·{" "}
                        {workWord("proposalWhy", {
                          why: p.signals.map((s) => (SIGNAL_WORD[s] ? workWord(SIGNAL_WORD[s]) : s)).join("、"),
                        })}
                        {" · "}
                        {left > 0 ? workWord("proposalLeaves", { n: left }) : workWord("proposalLeavesToday")}
                      </span>
                      {p.subject_status === "unknown" && (
                        <span className="work-sub">{nextWord("proposalNeedsYourDecision")}</span>
                      )}
                      {resolving === p.id ? (
                        <form className="work-actions" style={{ marginTop: 6 }} onSubmit={(ev) => {
                          ev.preventDefault()
                          const said = resolution.trim()
                          const source = evidence.trim()
                          if (!said || !source) return
                          setResolving(null)
                          setResolution("")
                          setEvidence("")
                          run(() => onResolve(p, said, source))
                        }}>
                          <input className="work-input" autoFocus value={resolution}
                            aria-label={nextWord("proposalResolveSummary")}
                            placeholder={nextWord("proposalResolveSummary")}
                            onChange={(ev) => setResolution(ev.target.value)} />
                          <input className="work-input" value={evidence}
                            aria-label={nextWord("proposalResolveEvidence")}
                            placeholder={nextWord("proposalResolveEvidence")}
                            onChange={(ev) => setEvidence(ev.target.value)} />
                          <button className="chip on" type="submit" disabled={busy || !resolution.trim() || !evidence.trim()}>
                            {nextWord("proposalResolveSubmit")}
                          </button>
                          <button className="chip" type="button" onClick={() => {
                            setResolving(null)
                            setResolution("")
                            setEvidence("")
                          }}>
                            {L.strings.webCancel}
                          </button>
                        </form>
                      ) : (
                        <div className="work-actions" style={{ marginTop: 6 }}>
                          <button className="chip on" type="button" disabled={busy} data-answer="track"
                            onClick={() => run(() => onAnswer(p, "track"))}>
                            {workWord("answerTrack")}
                          </button>
                          <button className="chip" type="button" disabled={busy} data-answer="later"
                            onClick={() => run(() => onAnswer(p, "later"))}>
                            {workWord("answerLater")}
                          </button>
                          <button className="chip" type="button" disabled={busy} data-answer="no"
                            onClick={() => run(() => onAnswer(p, "no"))}>
                            {nextWord("proposalNotNow")}
                          </button>
                          <button className="chip" type="button" disabled={busy} data-answer="resolve"
                            onClick={() => {
                              setResolving(p.id)
                              setResolution("")
                              setEvidence("")
                            }}>
                            {nextWord("proposalResolve")}
                          </button>
                        </div>
                      )}
                    </li>
                  )
                })}
              </ul>
            </div>
          ))
        )}
        {total > rows.length && <p className="work-note">{workWord("more", { n: total - rows.length })}</p>}
      </div>
    </details>
  )
}

/** The newest daily digest, folded: one line per thing that happened, only the ones that did. */
function DigestFold({ digest, read }: { digest: Digest | null; read: boolean }) {
  const b = digest?.body
  const lines: string[] = []
  if (b) {
    if (b.completed.total) lines.push(workWord("digestCompleted", { n: b.completed.total, landed: b.landed }))
    if (b.stalled.total) lines.push(workWord("digestStalled", { n: b.stalled.total }))
    if (b.from_backlog.total) lines.push(workWord("digestFromBacklog", { n: b.from_backlog.total }))
    if (b.automatic.total) lines.push(workWord("digestAutomatic", { n: b.automatic.total }))
    if (b.proposals_pending) lines.push(workWord("digestProposals", { n: b.proposals_pending }))
    if (b.decisions_open) lines.push(workWord("digestDecisions", { n: b.decisions_open }))
    if (b.awaiting_closure) lines.push(workWord("digestAwaiting", { n: b.awaiting_closure }))
    if (b.closure_asked.total) lines.push(workWord("digestClosureAsked", { n: b.closure_asked.total }))
    if (b.handed_off_todos) lines.push(workWord("digestHandedOff", { n: b.handed_off_todos }))
    if (b.backlog_stale.total) lines.push(workWord("digestBacklogStale", { n: b.backlog_stale.total }))
  }
  const date = digest ? digest.key.replace(/^daily:/, "") : ""
  return (
    <details className="work-fold" id="work-digest">
      <summary>
        <strong>{workWord("digestTitle")}</strong>
        {digest && <span className="work-count">{workWord("digestFor", { date })}</span>}
      </summary>
      <div className="work-fold-body">
        {!read ? (
          <p className="work-note">{workWord("unreadable")}</p>
        ) : !digest ? (
          <p>{workWord("digestNone")}</p>
        ) : (
          <ul className="work-lines">
            {lines.length === 0 && <li>{workWord("digestQuiet")}</li>}
            {lines.map((l) => (
              <li key={l}>{l}</li>
            ))}
            {b?.moves_truncated && <li className="work-note">{workWord("digestTruncated")}</li>}
          </ul>
        )}
      </div>
    </details>
  )
}
