import { useCallback, useEffect, useRef, useState } from "react"
import type { RestorableSession, RestorableSessions, RestoreResult } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { uuid } from "../legacy/js/core/util.js"
import {
  allOpened,
  allTicked,
  assistantName,
  lastSeenRelative,
  outcomeLine,
  outcomes,
  readOffer,
  restoreBatches,
  restoreBody,
  RestoreRefusal,
  rowName,
  sendDismissAll,
  sendRestore,
  type RowOutcome,
} from "./restore-offer.js"
import "./restore.css"

/**
 * The sessions a reboot took away, offered back where the empty list stands
 * (docs/session-restore.md). The decisions are `restore-offer.ts`'s; this draws them.
 */

const fetchNow = (input: string, init?: RequestInit) => globalThis.fetch(input, init)

/** A read closer than this to the last one is the same read: focus and visibility arrive together. */
const REREAD_GAP_MS = 1000

/**
 * What the machine has on offer, re-read when it could have changed: on
 * mount, when the line comes up (a hosted page asks before its seam is
 * attached, and changing machine there reloads the page), when the page comes
 * back into view or focus, and after a restore or a dismissal. Not on every
 * stream tick — a reboot does not happen between two of them.
 *
 * A read that fails keeps what was last read when the page could not reach
 * anything (`offline`) and drops it for a refusal: an offer is drawn only from
 * an answer.
 */
export function useRestoreOffer(live: boolean): { offer: RestorableSessions | null; refresh: () => void } {
  const [offer, setOffer] = useState<RestorableSessions | null>(null)
  const seq = useRef(0)
  const last = useRef(0)
  const refresh = useCallback((force = true) => {
    const now = Date.now()
    if (!force && now - last.current < REREAD_GAP_MS) return
    last.current = now
    const n = ++seq.current
    readOffer(fetchNow).then(
      (answer) => {
        if (n === seq.current) setOffer(answer)
      },
      (error: unknown) => {
        if (n !== seq.current) return
        if (!(error instanceof RestoreRefusal) || error.code !== "offline") setOffer(null)
      },
    )
  }, [])
  useEffect(() => {
    if (live) refresh()
  }, [live, refresh])
  useEffect(() => {
    refresh()
    const onVisible = () => {
      if (document.visibilityState === "visible") refresh(false)
    }
    const onFocus = () => refresh(false)
    document.addEventListener("visibilitychange", onVisible)
    window.addEventListener("focus", onFocus)
    return () => {
      document.removeEventListener("visibilitychange", onVisible)
      window.removeEventListener("focus", onFocus)
    }
  }, [refresh])
  return { offer, refresh: () => refresh() }
}

function cardTitle(n: number): string {
  return n === 1 ? nextWord("restoreCardTitleOne") : nextWord("restoreCardTitle", { n })
}

/**
 * The offer in the empty list's place. It stands inside `#list-empty` so the
 * home hero's picture and type are its own; the words the list says when there
 * is nothing to offer are not touched.
 */
export function RestoreHero({ offer, onOpen }: { offer: RestorableSessions; onOpen: () => void }) {
  return (
    <div className="restore-hero" data-restore="hero">
      <b>{cardTitle(offer.sessions.length)}</b>
      <span className="restore-hint">{nextWord("restoreCardHint")}</span>
      <button className="chip restore-open" type="button" onClick={onOpen}>
        {nextWord("restoreCardOpen")}
      </button>
    </div>
  )
}

/** The offer above rows the person already has. */
export function RestoreCard({ offer, onOpen }: { offer: RestorableSessions; onOpen: () => void }) {
  return (
    <div className="restore-card" data-restore="compact" role="region" aria-label={cardTitle(offer.sessions.length)}>
      <div className="restore-card-copy">
        <b>{cardTitle(offer.sessions.length)}</b>
        <span className="restore-hint">{nextWord("restoreCardHint")}</span>
      </div>
      <button className="chip restore-open" type="button" onClick={onOpen}>
        {nextWord("restoreCardOpen")}
      </button>
    </div>
  )
}

type Phase = "idle" | "restoring" | "dismissing"

/**
 * The sheet a card opens: each conversation on offer, ticked, and three ways
 * out. The rows are the ones on offer when it opened and stay put while it is
 * open, so a row that has just opened still says so rather than vanishing
 * from under the person's thumb when the list is re-read.
 */
export function RestoreSheet({
  sessions,
  onClose,
  onChanged,
}: {
  sessions: readonly RestorableSession[]
  onClose: () => void
  onChanged: () => void
}) {
  const [ticked, setTicked] = useState<Set<string>>(() => allTicked(sessions))
  const [rows, setRows] = useState<Map<string, RowOutcome>>(() => new Map())
  const [phase, setPhase] = useState<Phase>("idle")
  const [said, setSaid] = useState("")
  const goRef = useRef<HTMLButtonElement>(null)
  const busy = phase !== "idle"
  const acted = rows.size > 0
  const body = restoreBody(sessions, ticked)
  const count = body?.conversations.length ?? 0

  useEffect(() => {
    goRef.current?.focus()
  }, [])

  // Escape closes; while the sheet is open the list's own keys stand down,
  // as they do for the start sheet.
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") {
        if (!busy) onClose()
        ev.stopImmediatePropagation()
        return
      }
      if (ev.metaKey || ev.ctrlKey || ev.altKey) return
      if (ev.key !== "Tab" && ev.key !== "Enter" && ev.key !== " ") ev.stopImmediatePropagation()
    }
    document.addEventListener("keydown", onKey, true)
    return () => document.removeEventListener("keydown", onKey, true)
  }, [busy, onClose])

  const toggle = (id: string) => {
    if (busy || rows.get(id)?.kind === "opened") return
    setTicked((was) => {
      const next = new Set(was)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const restore = async () => {
    if (!body || busy) return
    const named = body.conversations
    const key = uuid()
    setPhase("restoring")
    setSaid("")
    setRows((was) => {
      const next = new Map(was)
      for (const id of named) next.set(id, { kind: "opening" })
      return next
    })
    const answered: RestoreResult[] = []
    let failure = ""
    let unknown = false
    for (const batch of restoreBatches(body, key)) {
      try {
        const answer = await sendRestore(fetchNow, { conversations: batch.conversations }, batch.key)
        answered.push(...answer.results)
      } catch (error) {
        if (error instanceof RestoreRefusal && error.code !== "offline" && error.status !== 0) {
          failure = nextWord("restoreRefused", { why: error.message })
        } else {
          unknown = true
        }
        break
      }
    }
    const settled = outcomes(named, { results: answered })
    // Rows the machine never answered for are not failures it reported: they
    // go back to idle, and the sentence says what is known.
    const answeredIds = new Set(answered.map((r) => r.conversation_id))
    setRows((was) => {
      const next = new Map(was)
      for (const id of named) next.set(id, answeredIds.has(id) ? settled.get(id)! : { kind: "idle" })
      return next
    })
    setTicked((was) => {
      const next = new Set(was)
      for (const id of named) if (settled.get(id)?.kind === "opened") next.delete(id)
      return next
    })
    setSaid(unknown ? nextWord("restoreUnknownOutcome") : failure)
    setPhase("idle")
    onChanged()
    if (!unknown && !failure && allOpened(named, settled)) onClose()
  }

  const dismissAll = async () => {
    if (busy) return
    setPhase("dismissing")
    setSaid("")
    try {
      await sendDismissAll(fetchNow, uuid())
      setPhase("idle")
      onChanged()
      onClose()
    } catch (error) {
      setPhase("idle")
      const why = error instanceof Error ? error.message : String(error)
      setSaid(error instanceof RestoreRefusal && error.code === "offline" ? nextWord("restoreUnknownOutcome") : nextWord("restoreRefused", { why }))
      onChanged()
    }
  }

  const locale = typeof document === "undefined" ? undefined : document.documentElement.lang || undefined
  const now = Date.now()
  return (
    <div className="overlay" id="restore" onClick={() => !busy && onClose()}>
      <div
        className="sheet restore-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="restore-title"
        onClick={(ev) => ev.stopPropagation()}
      >
        <h2 id="restore-title">{nextWord("restoreSheetTitle")}</h2>
        <div className="block">
          <p className="say">{nextWord("restoreSheetSay")}</p>
          <ul className="restore-list">
            {sessions.map((s) => {
              const outcome = rows.get(s.conversation_id)
              const opened = outcome?.kind === "opened"
              const on = ticked.has(s.conversation_id)
              const line = outcomeLine(outcome, (key, holes) => nextWord(key, holes))
              const seen = lastSeenRelative(s.last_seen, now, locale)
              return (
                <li key={s.conversation_id}>
                  <button
                    className={"chip check restore-row" + (on ? " on" : "")}
                    type="button"
                    role="checkbox"
                    aria-checked={on}
                    disabled={busy || opened}
                    data-outcome={outcome?.kind ?? "idle"}
                    onClick={() => toggle(s.conversation_id)}
                  >
                    <svg className="tick" viewBox="0 0 14 14" aria-hidden="true" focusable="false">
                      <rect className="box" x="0.5" y="0.5" width="13" height="13" rx="3.5"></rect>
                      <path className="mark" d="M3.6 7.1 5.9 9.4 10.4 4.6" strokeLinecap="round" strokeLinejoin="round"></path>
                    </svg>
                    <span className="restore-text">
                      <span className="restore-name">{rowName(s)}</span>
                      <span className="restore-meta">
                        <span>{assistantName(s.assistant)}</span>
                        <span className="restore-path" title={s.cwd}>{s.cwd}</span>
                        {seen && <span>{nextWord("restoreLastSeen", { time: seen })}</span>}
                      </span>
                      {line && <span className={"restore-outcome " + (outcome?.kind ?? "")}>{line}</span>}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
          <p className="said" role="status" aria-live="polite">{said}</p>
        </div>
        <div className="restore-buttons">
          <button className="chip restore-go" type="button" ref={goRef} disabled={busy || count === 0} onClick={restore}>
            {phase === "restoring" ? nextWord("restoreGoing") : nextWord("restoreGo", { n: count })}
          </button>
          <button className="chip" type="button" disabled={busy} onClick={dismissAll}>
            {phase === "dismissing" ? nextWord("restoreDismissing") : nextWord("restoreDismissAll")}
          </button>
          <button className="chip" type="button" disabled={busy} onClick={onClose}>
            {acted ? nextWord("restoreDone") : nextWord("restoreCancel")}
          </button>
        </div>
      </div>
    </div>
  )
}
