import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import type { Verification, VerificationCriterionState, VerificationData } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import {
  addNote,
  closeVerification,
  deleteVerification,
  markCriterion,
  readVerification,
  readVerifications,
} from "./verify/api.js"
import { comparisonNotes, comparisonRows, dueWords, splitRows, tally } from "./verify/model.js"
import { language, verifyWord, type VerifyWord } from "./verify/words.js"
import "./verify/verify.css"

/**
 * 驗收: what was changed and waits for someone to look again
 * (docs/verifications.md).
 *
 * A list of the open records, each with how long is left, and one record at
 * a time: why it exists, what would count as it holding, the data it is
 * judged by — read when the record is opened, never stored — and the notes
 * written on it, by the person here or by the scheduled task that reads the
 * data for them. Every change is one press against one record; there is no
 * way from here to touch more than one.
 *
 * The clock the countdown runs on is the daemon's (`at` on the list), moved
 * forward by how long the page has held it, so a phone whose clock is wrong
 * counts the same as the machine.
 */

/** How often a page on screen reads again: the board's own interval. */
const REFRESH_MS = 30_000

type ListState = { read: false; failure?: string } | { read: true; rows: Verification[]; at: number; heldSince: number }
type DetailState = { id: string; row?: Verification; data?: VerificationData; failure?: string }

const lang = language

function failureOf(e: unknown): string {
  return L.failureSentence(e, verifyWord("unreadable"))
}

function when(seconds: number): string {
  if (!seconds) return ""
  const d = new Date(seconds * 1000)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function VerifyPageView({ shown }: { shown: boolean }) {
  const T = L.strings
  const [list, setList] = useState<ListState>({ read: false })
  const [detail, setDetail] = useState<DetailState | null>(null)
  const [busy, setBusy] = useState(false)
  const [showClosed, setShowClosed] = useState(false)
  const [, tick] = useState(0)
  const ticket = useRef(0)
  const title = useRef<HTMLHeadingElement>(null)

  const load = useCallback(async () => {
    const mine = ++ticket.current
    setBusy(true)
    try {
      const answer = await readVerifications()
      if (mine !== ticket.current) return
      setList({ read: true, rows: answer.verifications, at: answer.at, heldSince: Date.now() })
    } catch (e) {
      if (mine !== ticket.current) return
      setList({ read: false, failure: failureOf(e) })
    } finally {
      if (mine === ticket.current) setBusy(false)
    }
  }, [])

  const open = useCallback(async (id: string) => {
    setDetail({ id })
    try {
      const answer = await readVerification(id)
      setDetail((d) => (d?.id === id ? { id, row: answer.verification, data: answer.data } : d))
    } catch (e) {
      setDetail((d) => (d?.id === id ? { id, failure: failureOf(e) } : d))
    }
  }, [])

  useEffect(() => {
    if (!shown) return
    void load()
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") void load()
      tick((n) => n + 1)
    }, REFRESH_MS)
    return () => clearInterval(timer)
  }, [shown, load])

  useLayoutEffect(() => {
    if (shown && !detail) title.current?.focus({ preventScroll: true })
  }, [shown, detail])

  const now = list.read ? list.at + Math.floor((Date.now() - list.heldSince) / 1000) : Math.floor(Date.now() / 1000)

  // A write answers the record; the list reads again so its row agrees.
  const changed = (row: Verification | null) => {
    if (row) setDetail((d) => (d && d.id === row.id ? { ...d, row } : d))
    else setDetail(null)
    void load()
  }

  return (
    <section
      id="verify"
      className="page board-page verify-page"
      data-page-view="verify"
      hidden={!shown}
      aria-labelledby="verify-title"
    >
      <header className="board-head">
        <div className="work-head-tools">
          <button className="board-button" id="verify-refresh" type="button" disabled={busy}
            onClick={() => { void load(); if (detail) void open(detail.id) }}>
            {busy ? T.webLoading : verifyWord("refresh")}
          </button>
        </div>
      </header>
      <div className="verify-wrap">
        {detail ? (
          <Detail state={detail} now={now} onBack={() => setDetail(null)} onChanged={changed} onReopen={open} />
        ) : (
          <>
            <div className="board-intro">
              <p className="board-eyebrow">{verifyWord("eyebrow")}</p>
              <h1 id="verify-title" tabIndex={-1} ref={title}>{verifyWord("title")}</h1>
              <p className="verify-lede">{verifyWord("lede")}</p>
            </div>
            <ListView list={list} now={now} showClosed={showClosed} onToggleClosed={() => setShowClosed((v) => !v)}
              onOpen={(id) => void open(id)} />
          </>
        )}
      </div>
    </section>
  )
}

function ListView({ list, now, showClosed, onToggleClosed, onOpen }: {
  list: ListState
  now: number
  showClosed: boolean
  onToggleClosed: () => void
  onOpen: (id: string) => void
}) {
  if (!list.read) {
    return list.failure ? <p className="verify-failure" role="status">{list.failure}</p> : null
  }
  const { open, closed } = splitRows(list.rows)
  return (
    <>
      {open.length ? (
        <ul className="verify-rows" id="verify-open">
          {open.map((row) => {
            const due = dueWords(lang(), row.due_at, now)
            const t = tally(row)
            return (
              <li key={row.id}>
                <button type="button" className="verify-row" onClick={() => onOpen(row.id)}>
                  <b>{row.title}</b>
                  <span className="verify-due" data-overdue={due.overdue || undefined}>{due.text}</span>
                  <span className="verify-meta">{verifyWord("tally", t)}</span>
                </button>
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="verify-empty">{verifyWord("listNone")}</p>
      )}
      {closed.length > 0 && (
        <div className="verify-closed">
          <button className="board-button" type="button" aria-expanded={showClosed} onClick={onToggleClosed}>
            {showClosed ? verifyWord("closedHide") : verifyWord("closedShow", { n: closed.length })}
          </button>
          {showClosed && (
            <ul className="verify-rows">
              {closed.map((row) => (
                <li key={row.id}>
                  <button type="button" className="verify-row" onClick={() => onOpen(row.id)}>
                    <b>{row.title}</b>
                    <span className="verify-meta">
                      {verifyWord(row.status === "accepted" ? "accepted" : "rejected")} ·{" "}
                      {verifyWord("closedOn", { when: when(row.closed_at) })}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </>
  )
}

const STATES: { state: VerificationCriterionState; word: VerifyWord }[] = [
  { state: "passed", word: "markPassed" },
  { state: "failed", word: "markFailed" },
  { state: "unset", word: "markUnset" },
]

function Detail({ state, now, onBack, onChanged, onReopen }: {
  state: DetailState
  now: number
  onBack: () => void
  onChanged: (row: Verification | null) => void
  onReopen: (id: string) => void
}) {
  const heading = useRef<HTMLHeadingElement>(null)
  const [note, setNote] = useState("")
  const [reason, setReason] = useState("")
  const [asking, setAsking] = useState(false)
  const [pending, setPending] = useState(false)
  const [said, setSaid] = useState("")

  useLayoutEffect(() => {
    heading.current?.focus({ preventScroll: true })
  }, [state.id])

  const act = async (work: () => Promise<Verification | null | void>) => {
    setPending(true)
    setSaid("")
    try {
      const row = await work()
      if (row !== undefined) onChanged(row)
      return true
    } catch (e) {
      setSaid(verifyWord("failedAction", { why: failureOf(e) }))
      return false
    } finally {
      setPending(false)
    }
  }

  const back = (
    <button className="board-button verify-back" type="button" onClick={onBack}>
      ← {verifyWord("back")}
    </button>
  )
  const row = state.row
  if (!row) {
    return (
      <div className="verify-detail">
        {back}
        {state.failure ? <p className="verify-failure" role="status">{state.failure}</p> : <p>{L.strings.webLoading}</p>}
      </div>
    )
  }
  const isOpen = row.status === "open"
  const due = dueWords(lang(), row.due_at, now)

  return (
    <article className="verify-detail" id="verify-detail" data-status={row.status}>
      {back}
      <header className="verify-detail-head">
        <h1 tabIndex={-1} ref={heading}>{row.title}</h1>
        <p className="verify-meta">
          <span className="verify-status">{verifyWord(isOpen ? "open" : row.status === "accepted" ? "accepted" : "rejected")}</span>
          {isOpen && <span className="verify-due" data-overdue={due.overdue || undefined}>{due.text}</span>}
          <span>{verifyWord("dueAt", { when: when(row.due_at) })}</span>
          <span>{verifyWord("startedAt", { when: when(row.started_at) })}</span>
          {!isOpen && <span>{verifyWord("closedOn", { when: when(row.closed_at) })}</span>}
        </p>
        {!isOpen && row.close_reason && <p className="verify-reason">{row.close_reason}</p>}
      </header>

      <section className="verify-block">
        <h2>{verifyWord("why")}</h2>
        <p className="verify-text">{row.why || verifyWord("whyNone")}</p>
      </section>

      <section className="verify-block">
        <h2>{verifyWord("criteria")}</h2>
        {row.criteria.length === 0 && <p className="verify-empty">{verifyWord("criteriaNone")}</p>}
        <ol className="verify-criteria">
          {row.criteria.map((c) => (
            <li key={c.index} data-state={c.state}>
              <div className="verify-criterion">
                <span className="verify-mark" aria-hidden="true">{c.state === "passed" ? "✓" : c.state === "failed" ? "✗" : "·"}</span>
                <span className="verify-text">{c.text}</span>
                <span className="verify-state">{verifyWord(c.state)}</span>
              </div>
              {isOpen && (
                <div className="verify-actions">
                  {STATES.filter((s) => s.state !== c.state).map((s) => (
                    <button key={s.state} className="board-button" type="button" disabled={pending}
                      onClick={() => void act(async () => (await markCriterion(row.id, c.index, s.state)).verification)}>
                      {verifyWord(s.word)}
                    </button>
                  ))}
                </div>
              )}
            </li>
          ))}
        </ol>
      </section>

      <section className="verify-block">
        <h2>{verifyWord("data")}</h2>
        <DataView data={state.data} hasSource={!!row.source} />
      </section>

      <section className="verify-block">
        <h2>{verifyWord("notes")}</h2>
        {row.notes.length === 0 && <p className="verify-empty">{verifyWord("notesNone")}</p>}
        <ul className="verify-notes">
          {row.notes.map((n) => (
            <li key={n.id}>
              <p className="verify-meta">
                <b>{n.author_kind === "person" ? verifyWord("byPerson") : n.author}</b>
                <span>{when(n.at)}</span>
              </p>
              <p className="verify-text">{n.text}</p>
            </li>
          ))}
        </ul>
        <form className="verify-form" onSubmit={(ev) => {
          ev.preventDefault()
          const text = note.trim()
          if (!text) return
          void act(async () => {
            await addNote(row.id, text)
            setNote("")
            onReopen(row.id)
          })
        }}>
          <textarea value={note} rows={3} placeholder={verifyWord("notePlaceholder")} aria-label={verifyWord("notePlaceholder")}
            onChange={(ev) => setNote(ev.target.value)} />
          <div className="verify-actions">
            <button className="board-button" type="submit" disabled={pending || !note.trim()}>{verifyWord("noteAdd")}</button>
          </div>
        </form>
      </section>

      {isOpen && (
        <section className="verify-block">
          <h2>{verifyWord("verdict")}</h2>
          <textarea value={reason} rows={2} placeholder={verifyWord("reasonPlaceholder")} aria-label={verifyWord("reasonPlaceholder")}
            onChange={(ev) => setReason(ev.target.value)} />
          <div className="verify-actions">
            {(["accepted", "rejected"] as const).map((status) => (
              <button key={status} className="board-button" type="button" id={"verify-" + status} disabled={pending}
                data-tone={status === "accepted" ? "ok" : "bad"}
                onClick={() => {
                  if (!reason.trim()) return setSaid(verifyWord("reasonNeeded"))
                  void act(async () => (await closeVerification(row.id, status, reason.trim())).verification)
                }}>
                {verifyWord(status === "accepted" ? "accept" : "reject")}
              </button>
            ))}
          </div>
        </section>
      )}

      {said && <p className="verify-failure" role="status">{said}</p>}

      <section className="verify-block verify-danger">
        {asking ? (
          <div role="alertdialog" aria-labelledby="verify-remove-ask">
            <p id="verify-remove-ask">{verifyWord(isOpen ? "removeAskOpen" : "removeAsk")}</p>
            <div className="verify-actions">
              <button className="board-button" type="button" data-tone="bad" disabled={pending}
                onClick={() => void act(async () => {
                  await deleteVerification(row)
                  return null
                }).then((ok) => ok || setAsking(false))}>
                {verifyWord("removeYes")}
              </button>
              <button className="board-button" type="button" onClick={() => setAsking(false)}>{verifyWord("cancel")}</button>
            </div>
          </div>
        ) : (
          <button className="board-button" type="button" id="verify-remove" onClick={() => setAsking(true)}>
            {verifyWord("remove")}
          </button>
        )}
      </section>
    </article>
  )
}

function DataView({ data, hasSource }: { data?: VerificationData; hasSource: boolean }) {
  if (!hasSource || !data) return <p className="verify-empty">{verifyWord("dataNone")}</p>
  if (data.error) return <p className="verify-failure">{verifyWord("dataUnreadable", { why: data.error })}</p>
  const c = data.compaction_compare
  if (!c) return <p className="verify-empty">{verifyWord("dataNone")}</p>
  const rows = comparisonRows(lang(), c)
  const notes = comparisonNotes(lang(), c)
  const heads: VerifyWord[] = ["colSessions", "colTasks", "colCost", "colCalls", "colCompactions", "colAbove",
    "colSuccess", "colStalled", "colRespawns"]
  return (
    <>
      <p className="verify-meta">{verifyWord("dataRange", { since: when(c.since), until: when(c.until) })}</p>
      {rows.length === 0 ? (
        <p className="verify-empty">{verifyWord("dataEmpty")}</p>
      ) : (
        // The table scrolls inside its own box on a phone; the page never does.
        <div className="verify-table" role="region" tabIndex={0} aria-label={verifyWord("data")}>
          <table>
            <thead>
              <tr>
                <th scope="col">{verifyWord("colGroup")}</th>
                {heads.map((h) => <th key={h} scope="col">{verifyWord(h)}</th>)}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.group}>
                  <th scope="row">{r.group}</th>
                  {r.cells.map((cell, i) => <td key={i}>{cell}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > 0 && <p className="verify-note">{verifyWord("costNote")}</p>}
      {notes.map((n) => <p key={n} className="verify-note">{n}</p>)}
    </>
  )
}

export const page: PageModule = { id: "verify", Component: VerifyPageView }
