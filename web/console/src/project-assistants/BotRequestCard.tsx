import {useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import {
  BotRequestRefused, actorName, botOf, checklistProgress, handoffHistory, lastUpdated, noteParts, progressLabel, progressOf,
  readBotRequest, readBotRequests, refusalWords, requestBotName, writeBotRequest, type BotRequestAction,
} from "./bot-request"
import type {BotDirectoryState} from "./open-bot"
import type {Words} from "./words"
import "./bot-request.css"

const UNKNOWN_APPEARANCE: W.ProjectAssistantAppearance = {shape: "blob", color: "#9ca6b3"}

export type BotRequestReading =
  | {state: "loading"} | {state: "hidden"} | {state: "failed"}
  | {state: "ready"; cards: W.BotRequestView[]}

/**
 * The cards of one Bot, or of every Bot. `hidden` when disabled or when the
 * origin has no card routes; `failed` keeps the last cards out of sight rather
 * than showing an old list as current.
 */
export function useBotRequests({enabled, bot, state, read = readBotRequests}: {
  enabled: boolean; bot?: string; state?: string; read?: typeof readBotRequests
}): {reading: BotRequestReading; refresh: () => void} {
  const [reading, setReading] = useState<BotRequestReading>(enabled ? {state: "loading"} : {state: "hidden"})
  const [tick, setTick] = useState(0)
  const refresh = useCallback(() => setTick(value => value + 1), [])
  useEffect(() => {
    if (!enabled) { setReading({state: "hidden"}); return }
    const control = new AbortController()
    void read({bot, state}, control.signal).then(
      cards => { if (!control.signal.aborted) setReading(cards ? {state: "ready", cards} : {state: "hidden"}) },
      () => { if (!control.signal.aborted) setReading({state: "failed"}) },
    )
    return () => control.abort()
  }, [enabled, bot, state, read, tick])
  useEffect(() => {
    if (!enabled) return
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") refresh() }, 30_000)
    return () => window.clearInterval(timer)
  }, [enabled, refresh])
  return {reading, refresh}
}

/** The state label: a dot for the light and the state in words, so colour is never the only signal. */
export function BotRequestState({card, w}: {card: W.BotRequestView; w: Words}) {
  const progress = progressOf(card)
  return <span className="pa-request-state" data-progress={progress}><span className="pa-request-dot" aria-hidden="true"/>{progressLabel(progress, w)}</span>
}

function Holder({card, bots, botDirectoryState, w, size}: {
  card: W.BotRequestView; bots: readonly W.ProjectAssistantBot[]; botDirectoryState: BotDirectoryState; w: Words; size: number
}) {
  const bot = botOf(card.baton.bot, bots)
  return <span className="pa-request-holder">
    <BotAvatar appearance={bot?.appearance ?? UNKNOWN_APPEARANCE} size={size}/>
    <span className="pa-request-holder-name">{requestBotName(bot, w, botDirectoryState)}</span>
  </span>
}

/** One row of the Board's Bot work section. */
export function BotRequestRow({card, bots, botDirectoryState = "ready", w, now, onOpen}: {
  card: W.BotRequestView; bots: readonly W.ProjectAssistantBot[]; botDirectoryState?: BotDirectoryState
  w: Words; now: number; onOpen: (card: W.BotRequestView, opener: HTMLElement) => void
}) {
  const progress = checklistProgress(card, w), updated = lastUpdated(card.updated_at, now, w)
  return <button type="button" className="pa-request-row" data-request-id={card.id} data-progress={progressOf(card)}
    onClick={event => onOpen(card, event.currentTarget)}>
    <Holder card={card} bots={bots} botDirectoryState={botDirectoryState} w={w} size={28}/>
    <strong className="pa-request-title">{card.title}</strong>
    <span className="pa-request-facts">
      <BotRequestState card={card} w={w}/>
      {progress && <span className="pa-request-progress">{progress}</span>}
      {updated && <time dateTime={card.updated_at}>{updated}</time>}
    </span>
  </button>
}

/** The card's one compact line in a Bot's chat, above its work result cards. */
export function BotRequestLine({card, w, onOpen}: {card: W.BotRequestView; w: Words; onOpen: (card: W.BotRequestView, opener: HTMLElement) => void}) {
  const progress = checklistProgress(card, w)
  return <button type="button" className="pa-request-line" data-request-id={card.id} data-progress={progressOf(card)}
    aria-label={w.requestOpen.replace("{title}", card.title)} onClick={event => onOpen(card, event.currentTarget)}>
    <strong className="pa-request-title">{card.title}</strong>
    <BotRequestState card={card} w={w}/>
    {progress && <span className="pa-request-progress">{progress}</span>}
    <span aria-hidden="true">›</span>
  </button>
}

function Note({text}: {text: string}) {
  return <span className="pa-request-note">{noteParts(text).map((part, index) => part.href
    ? <a key={index} href={part.href} target="_blank" rel="noreferrer noopener">{part.text}</a> : part.text)}</span>
}

type HistoryState = "loading" | "ready" | "failed"

function RequestHistory({card, state, nameActor, time, w, onRetry}: {
  card: W.BotRequestView; state: HistoryState; nameActor: (actor: string | undefined) => string
  time: (at: string) => string; w: Words; onRetry: () => void
}) {
  const handoffs = handoffHistory(card)
  return <section aria-label={w.requestHistory}>
    <h3>{w.requestHistory}</h3>
    {state === "loading" ? <p className="pa-request-quiet" role="status">{w.loading}</p>
      : state === "failed" ? <p className="pa-request-quiet" role="alert">{w.requestHistoryFailed} <button type="button" className="pa-request-text-button" onClick={onRetry}>{w.retry}</button></p>
        : handoffs.length === 0 ? <p className="pa-request-quiet">{w.requestNoHistory}</p>
          : <ol className="pa-request-history">{handoffs.map(row => <li key={row.id}>
            <span>{row.to ? w.requestHandedTo.replace("{from}", nameActor(row.from)).replace("{to}", nameActor(row.to))
              : w.requestHandedBy.replace("{from}", nameActor(row.from))}</span>
            <time dateTime={row.at}>{time(row.at)}</time>
            {row.note && <Note text={row.note}/>}
          </li>)}</ol>}
  </section>
}

type Mode = "view" | "handoff" | "complete" | "cancel"
const ROLES = ["maker", "reviewer", "integrator"] as const

/**
 * A card's detail: a dialog on desktop, a bottom sheet at phone width. It
 * rereads the card for its history, and after every write or version conflict.
 */
export function BotRequestDetail({card: listed, bots, botDirectoryState = "ready", links = [], w, language, onClose, onChanged, onOpenChat, onOpenWork, boardItem, read = readBotRequest, write = writeBotRequest, initialMode = "view"}: {
  card: W.BotRequestView
  bots: readonly W.ProjectAssistantBot[]
  botDirectoryState?: BotDirectoryState
  /** The card's dispatched Sessions, where the opener knows them. */
  links?: readonly W.ProjectAssistantWorkLink[]
  w: Words
  language: string
  onClose: () => void
  onChanged?: (card: W.BotRequestView) => void
  /** Opens the holder Bot's chat; absent when that Bot cannot be opened from here. */
  onOpenChat?: (card: W.BotRequestView) => void
  onOpenWork?: (work: W.ProjectAssistantWorkLink, view?: "session") => void
  /** The linked Board item's chip, rendered by the opener that can open it. */
  boardItem?: ReactNode
  read?: typeof readBotRequest
  write?: typeof writeBotRequest
  /** The form the detail opens on: a held card's Hand on or Check and complete press opens straight on it. */
  initialMode?: "view" | "handoff" | "complete"
}) {
  const dialog = useRef<HTMLDialogElement>(null), titleID = useId(), formID = useId()
  const [card, setCard] = useState(listed)
  const [history, setHistory] = useState<HistoryState>(listed.events ? "ready" : "loading")
  const [mode, setMode] = useState<Mode>(initialMode)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState("")
  const [reason, setReason] = useState(""), [note, setNote] = useState("")
  const [toBot, setToBot] = useState(""), [role, setRole] = useState<string>("maker")
  const key = useRef(""), alive = useRef(true)
  const changed = useRef(onChanged)
  changed.current = onChanged

  useLayoutEffect(() => {
    const node = dialog.current, opener = document.activeElement
    alive.current = true
    if (node && !node.open) node.showModal?.()
    return () => {
      alive.current = false
      node?.close?.()
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus({preventScroll: true})
    }
  }, [])
  const reread = useCallback(async () => {
    try {
      const fresh = await read(listed.id)
      if (!alive.current) return
      setCard(fresh); setHistory("ready"); changed.current?.(fresh)
    } catch {
      if (alive.current) setHistory("failed")
    }
  }, [listed.id, read])
  useEffect(() => { void reread() }, [reread])

  const open = card.state === "open", waiting = progressOf(card) === "waiting"
  const openEntries = card.checklist.some(entry => entry.state === "open")
  const candidates = bots.filter(bot => !bot.hidden && bot.key.bot_id !== card.baton.bot)
  const time = (at: string | undefined) => {
    const value = at ? Date.parse(at) : NaN
    return Number.isFinite(value) ? new Intl.DateTimeFormat(language, {month: "short", day: "numeric", hour: "numeric", minute: "2-digit"}).format(value) : ""
  }
  const enter = (next: Mode) => { key.current = ""; setFailure(""); setMode(next) }
  const send = async (action: BotRequestAction, body: Omit<W.BotRequestPersonWrite, "expected_version">) => {
    if (busy) return
    key.current ||= crypto.randomUUID()
    setBusy(true); setFailure("")
    try {
      const result = await write(card.id, action, {...body, expected_version: card.version}, key.current)
      if (!alive.current) return
      key.current = ""
      setCard(result.card); setHistory(result.card.events ? "ready" : history); changed.current?.(result.card)
      setMode("view"); setReason(""); setNote(""); setToBot("")
    } catch (error) {
      if (!alive.current) return
      const code = error instanceof BotRequestRefused ? error.code : "unconfirmed"
      // Only an unanswered write keeps its key: sending it again asks about the same decision.
      if (code !== "unconfirmed") key.current = ""
      setFailure(refusalWords(code, w))
      if (code === "bot_request_version_conflict" || code === "unconfirmed") void reread()
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (mode === "handoff") void send("handoff", {to_bot: toBot, role, note: note.trim()})
    else if (mode === "complete") void send("complete", {reason: reason.trim()})
    else if (mode === "cancel") void send("cancel", {reason: reason.trim()})
  }
  const roleWord: Record<string, string> = {maker: w.requestRoleMaker, reviewer: w.requestRoleReviewer, integrator: w.requestRoleIntegrator}
  const verdictWord: Record<string, string> = {pass: w.requestVerdictPass, conditional_pass: w.requestVerdictConditional, reject: w.requestVerdictReject}
  const verdict = card.verdicts?.at(-1)
  const nameActor = (actor: string | undefined) => actorName(actor, bots, w, botDirectoryState)

  return <dialog ref={dialog} className="pa-request-detail" aria-labelledby={titleID} data-request-id={card.id}
    onCancel={event => { event.preventDefault(); if (!busy) onClose() }}>
    <header className="pa-request-detail-head">
      <div>
        <small>{w.requestCard}</small>
        <h2 id={titleID}>{card.title}</h2>
      </div>
      <button type="button" className="pa-request-close" aria-label={w.close} disabled={busy} onClick={onClose}>
        <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8"/></svg>
      </button>
    </header>
    <div className="pa-request-detail-body">
      <p className="pa-request-summary">
        <BotRequestState card={card} w={w}/>
        {checklistProgress(card, w) && <span className="pa-request-progress">{checklistProgress(card, w)}</span>}
      </p>
      {card.description && <p className="pa-request-description">{card.description}</p>}

      <section aria-label={w.requestHolder}>
        <h3>{w.requestHolder}</h3>
        <Holder card={card} bots={bots} botDirectoryState={botDirectoryState} w={w} size={32}/>
      </section>

      <RequestHistory card={card} state={history} nameActor={nameActor} time={time} w={w}
        onRetry={() => { setHistory("loading"); void reread() }}/>

      <section aria-label={w.requestChecklist}>
        <h3>{w.requestChecklist}</h3>
        {card.checklist.length === 0 ? <p className="pa-request-quiet">{w.requestNoChecklist}</p>
          : <ul className="pa-request-checklist">{card.checklist.map(entry => {
            const state = entry.state === "done" ? "done" : entry.state === "not_needed" ? "not_needed" : "open"
            const stateWord = state === "done" ? w.requestEntryDone : state === "not_needed" ? w.requestEntryNotNeeded : w.requestEntryOpen
            return <li key={entry.key} data-entry-state={state}>
              <span className="pa-request-box" aria-hidden="true">{state === "done" ? "✓" : state === "not_needed" ? "–" : ""}</span>
              <div>
                <span className="pa-request-entry-label">{state === "not_needed" ? <s>{entry.label}</s> : entry.label}<span className="pa-request-sr"> {stateWord}</span></span>
                {state === "not_needed" && <small className="pa-request-entry-state">{stateWord}</small>}
                {entry.note && <Note text={entry.note}/>}
                {state !== "open" && entry.reported_by && <small className="pa-request-reported">{w.requestReported
                  .replace("{bot}", nameActor(entry.reported_by)).replace("{time}", time(entry.reported_at))}</small>}
              </div>
            </li>
          })}</ul>}
      </section>

      {verdict && <p className="pa-request-verdict" data-verdict={verdict.verdict}>
        <strong>{w.requestVerdict}</strong> {verdictWord[verdict.verdict] ?? verdict.verdict}
        {" · "}{nameActor(verdict.reviewer_bot)}{verdict.commit && <> · <code title={verdict.commit}>{verdict.commit.slice(0, 8)}</code></>}
        {verdict.text && <Note text={verdict.text}/>}
      </p>}

      {boardItem && <section aria-label={w.requestBoardItem}><h3>{w.requestBoardItem}</h3>{boardItem}</section>}

      {links.length > 0 && <section aria-label={w.dispatchedStripLabel}>
        <h3>{w.dispatchedStripLabel}</h3>
        <ul className="pa-request-sessions">{links.map(link => <li key={link.id}>
          {onOpenWork ? <button type="button" className="pa-request-text-button" onClick={() => onOpenWork(link, link.worker_terminal && link.worker_session ? "session" : undefined)}>{link.title || link.task_id}</button>
            : <span>{link.title || link.task_id}</span>}
          <small>{w[link.state] ?? link.state}</small>
        </li>)}</ul>
      </section>}
    </div>

    {mode !== "view" && <form id={formID} className="pa-request-form" onSubmit={submit}>
      {mode === "handoff" && <>
        <label>{w.requestHandoffTo}<select required value={toBot} disabled={busy} onChange={event => { key.current = ""; setToBot(event.target.value) }}>
          <option value="" disabled>{w.requestHandoffChoose}</option>
          {candidates.map(bot => <option key={bot.key.bot_id} value={bot.key.bot_id}>{bot.name}</option>)}
        </select></label>
        <label>{w.requestHandoffRole}<select value={role} disabled={busy} onChange={event => { key.current = ""; setRole(event.target.value) }}>
          {ROLES.map(value => <option key={value} value={value}>{roleWord[value]}</option>)}
        </select></label>
        <label>{w.requestHandoffNote}<textarea rows={2} maxLength={2000} value={note} disabled={busy} onChange={event => { key.current = ""; setNote(event.target.value) }}/></label>
      </>}
      {mode === "complete" && <label>{w.requestCompleteReason}
        <textarea rows={2} required maxLength={2000} value={reason} disabled={busy} autoFocus onChange={event => { key.current = ""; setReason(event.target.value) }}/></label>}
      {mode === "cancel" && <p>{w.requestCancelConfirm}</p>}
    </form>}
    {failure && <p className="pa-request-failure" role="alert">{failure}</p>}
    <footer className="pa-request-actions">
      {mode === "view" ? <>
        {waiting && <button type="button" className="pa-request-primary" disabled={!onOpenChat} onClick={() => onOpenChat?.(card)}>
          {w.requestOpenChat.replace("{bot}", nameActor(card.baton.bot))}</button>}
        {open && <>
          <button type="button" className="pa-request-danger" disabled={busy} onClick={() => enter("cancel")}>{w.requestCancel}</button>
          <button type="button" disabled={busy || candidates.length === 0} title={candidates.length === 0 ? w.requestHandoffNone : undefined} onClick={() => enter("handoff")}>{w.requestHandoff}</button>
          <button type="button" className={waiting ? undefined : "pa-request-primary"} disabled={busy} aria-busy={busy || undefined}
            onClick={() => openEntries ? enter("complete") : void send("complete", {})}>{w.requestComplete}</button>
        </>}
        {!open && !waiting && <button type="button" onClick={onClose}>{w.close}</button>}
      </> : <>
        <button type="button" disabled={busy} onClick={() => enter("view")}>{w.requestBack}</button>
        <button type="submit" form={formID} className={mode === "cancel" ? "pa-request-danger" : "pa-request-primary"} disabled={busy || mode === "handoff" && !toBot || mode === "complete" && !reason.trim()} aria-busy={busy || undefined}>
          {mode === "handoff" ? w.requestHandoffSend : mode === "cancel" ? w.requestCancel : w.requestComplete}</button>
      </>}
    </footer>
  </dialog>
}
