import {createContext, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode} from "react"
import type * as W from "@clawdline/contract"
import {actorName, botRequestFetcher, readBotRequest, writeBotRequest, cardFrom, checklistProgress, heldCards, heldCounts, lastUpdated, progressLabel, progressOf, readBotRequests, stalledWords, type BotRequestProgress} from "./bot-request"
import type {SourceClient} from "./client"
import type {BotDirectoryState} from "./open-bot"
import type {Words} from "./words"
import "./held-cards.css"

/** What a press on a held card asks for; the chat decides how each is carried out. */
export type HeldCardAction = "retry" | "handoff" | "complete" | "view"

const PHONE = "(max-width: 640px)"

function usePhone(): boolean {
  const query = typeof window !== "undefined" && typeof window.matchMedia === "function" ? window.matchMedia(PHONE) : null
  const [phone, setPhone] = useState(() => query?.matches ?? false)
  useEffect(() => {
    if (!query) return
    const change = () => setPhone(query.matches)
    query.addEventListener?.("change", change)
    return () => query.removeEventListener?.("change", change)
  }, [query])
  return phone
}

const say = (text: string, fields: Record<string, string | number>) =>
  Object.entries(fields).reduce((out, [key, value]) => out.replaceAll(`{${key}}`, String(value)), text)

function CardIcon() {
  return <svg className="pa-held-icon" viewBox="0 0 18 18" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="3" y="4" width="10" height="11" rx="2"/><path d="M6 2.5h7a2 2 0 0 1 2 2V12"/><path d="M5.6 9.6l1.7 1.7 3.2-3.4"/>
  </svg>
}

function HeldRow({card, bots, botDirectoryState, botName, writable, w, now, onAction}: {
  card: W.BotRequestView; bots: readonly W.ProjectAssistantBot[]; botName: string; writable: boolean; w: Words; now: number
  botDirectoryState: BotDirectoryState
  onAction: (card: W.BotRequestView, action: HeldCardAction) => void
}) {
  const progress = progressOf(card), from = cardFrom(card), checked = checklistProgress(card, w), updated = lastUpdated(card.updated_at, now, w)
  const role = {maker: w.requestRoleMaker, reviewer: w.requestRoleReviewer, integrator: w.requestRoleIntegrator}[card.baton.role] ?? ""
  const who = actorName(from.actor, bots, w, botDirectoryState)
  const actions: ReactNode[] = []
  if (progress === "stalled") {
    if (writable) actions.push(<button key="retry" type="button" className="pa-held-primary" onClick={() => onAction(card, "retry")}>{say(w.heldRetry, {name: botName})}</button>,
      <button key="handoff" type="button" onClick={() => onAction(card, "handoff")}>{w.heldHandOff}</button>)
    actions.push(<button key="view" type="button" onClick={() => onAction(card, "view")}>{w.heldView}</button>)
  } else if (progress === "waiting") {
    if (writable) actions.push(<button key="complete" type="button" className="pa-held-primary" onClick={() => onAction(card, "complete")}>{w.heldReview}</button>)
    actions.push(<button key="view" type="button" onClick={() => onAction(card, "view")}>{w.heldView}</button>)
  }
  const head = <>
    <span className="pa-held-dot" data-progress={progress} aria-hidden="true"/>
    <strong className="pa-held-title">{card.title}</strong>
    <span className="pa-held-state" data-progress={progress}>{progressLabel(progress, w)}</span>
    <span className="pa-held-meta">
      {[role, from.handed ? say(w.heldHandedBy, {name: who}) : say(w.heldCreatedBy, {name: who})].filter(Boolean).join(" · ")}
      {checked && <span className="pa-held-progress">{checked}</span>}
      {updated && <time dateTime={card.updated_at}>{updated}</time>}
    </span>
  </>
  return <li className="pa-held-row" data-progress={progress} data-request-id={card.id}>
    {actions.length ? <div className="pa-held-row-head">{head}</div>
      : <button type="button" className="pa-held-row-head" aria-label={w.requestOpen.replace("{title}", card.title)} onClick={() => onAction(card, "view")}>{head}</button>}
    {progress === "stalled" && <p className="pa-held-reason">{stalledWords(card, w)}</p>}
    {actions.length > 0 && <div className="pa-held-actions">{actions}</div>}
  </li>
}

/**
 * The Bot request cards a Bot holds, at the top of its chat: one line that
 * counts them, opening a list on a desktop and a bottom sheet on a phone.
 * Nothing is drawn while the Bot holds no open card.
 */
export function HeldCards({cards, bot, botName, bots, botDirectoryState = "ready", writable, w, onAction, onOpenBoard}: {
  cards: readonly W.BotRequestView[]
  bot: string
  botName: string
  bots: readonly W.ProjectAssistantBot[]
  botDirectoryState?: BotDirectoryState
  /** Whether this device may send the person's writes to the Bot's machine. */
  writable: boolean
  w: Words
  onAction: (card: W.BotRequestView, action: HeldCardAction) => void
  /** Opens the Board's Bot work; absent where the Board does not list it. */
  onOpenBoard?: () => void
}) {
  const {open, done} = heldCards(cards, bot)
  const [expanded, setExpanded] = useState(false), [showDone, setShowDone] = useState(false)
  const phone = usePhone(), listID = useId(), titleID = useId()
  const sheet = useRef<HTMLDialogElement>(null), strip = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null)
  const now = Date.now()
  const stalled = open.filter(card => progressOf(card) === "stalled").length
  const waiting = open.filter(card => progressOf(card) === "waiting").length
  const worst = stalled ? "stalled" : waiting ? "waiting" : "in_progress"

  useLayoutEffect(() => {
    const node = sheet.current
    if (!phone || !node) return
    if (expanded && !node.open) node.showModal?.()
    if (!expanded && node.open) node.close?.()
  }, [expanded, phone])
  useEffect(() => {
    if (!expanded || phone) return
    const away = (event: PointerEvent) => {
      const target = event.target as Node
      if (!panel.current?.contains(target) && !strip.current?.contains(target)) setExpanded(false)
    }
    const key = (event: KeyboardEvent) => { if (event.key === "Escape") { setExpanded(false); strip.current?.focus() } }
    document.addEventListener("pointerdown", away); document.addEventListener("keydown", key)
    return () => { document.removeEventListener("pointerdown", away); document.removeEventListener("keydown", key) }
  }, [expanded, phone])
  if (!open.length) return null

  const act = (card: W.BotRequestView, action: HeldCardAction) => {
    setExpanded(false); onAction(card, action)
    // Ask again fills the chat's composer; the person reads it and sends it.
    if (action === "retry") window.setTimeout(() => strip.current?.closest(".pa-chat")?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus())
  }
  const list = <>
    <ul className="pa-held-list">{open.map(card => <HeldRow key={card.id} card={card} bots={bots} botDirectoryState={botDirectoryState} botName={botName} writable={writable} w={w} now={now} onAction={act}/>)}</ul>
    {showDone && done.length > 0 && <ul className="pa-held-list pa-held-done">{done.map(card => <HeldRow key={card.id} card={card} bots={bots} botDirectoryState={botDirectoryState} botName={botName} writable={false} w={w} now={now} onAction={act}/>)}</ul>}
    {(done.length > 0 || onOpenBoard) && <footer className="pa-held-foot">
      {done.length > 0 ? <button type="button" aria-expanded={showDone} onClick={() => setShowDone(value => !value)}>{say(w.heldDone, {count: done.length})} ›</button> : <span/>}
      {onOpenBoard && <button type="button" onClick={() => { setExpanded(false); onOpenBoard() }}>{w.heldBoard} ›</button>}
    </footer>}
  </>
  return <div className="pa-held" data-open={expanded || undefined}>
    <button ref={strip} type="button" className="pa-held-strip" data-progress={worst} aria-expanded={expanded} aria-controls={listID}
      aria-label={say(w.heldShow, {name: botName})} onClick={() => setExpanded(value => !value)}>
      <CardIcon/>
      <strong>{say(phone ? w.heldSummaryShort : w.heldSummary, {count: open.length})}</strong>
      {stalled > 0 && <span className="pa-held-count" data-progress="stalled"><span className="pa-held-dot" data-progress="stalled" aria-hidden="true"/>{say(phone ? w.heldStalledShort : w.heldStalled, {count: stalled})}</span>}
      {waiting > 0 && <span className="pa-held-count" data-progress="waiting"><span className="pa-held-dot" data-progress="waiting" aria-hidden="true"/>{say(phone ? w.heldWaitingShort : w.heldWaiting, {count: waiting})}</span>}
      <svg className="pa-held-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>
    </button>
    {phone ? <dialog ref={sheet} id={listID} className="pa-held-sheet" aria-labelledby={titleID}
      onCancel={event => { event.preventDefault(); setExpanded(false) }} onClick={event => { if (event.target === event.currentTarget) setExpanded(false) }}>
      <div className="pa-held-grip" aria-hidden="true"/>
      <header className="pa-held-sheet-head"><h2 id={titleID}>{say(w.heldSheetTitle, {name: botName})}</h2><span>{say(w.heldOpenCount, {count: open.length})}</span></header>
      {list}
    </dialog> : expanded && <div ref={panel} id={listID} className="pa-held-panel" role="region" aria-label={say(w.heldSheetTitle, {name: botName})}>{list}</div>}
  </div>
}

/** The roster row's chip: how many open cards the Bot holds, lit by the most urgent. */
export function HeldChip({count, progress, w}: {count: number; progress: string; w: Words}) {
  return <span className="pa-held-chip" data-progress={progress} title={say(w.heldChipLabel, {count})} aria-label={say(w.heldChipLabel, {count})}>
    <span className="pa-held-dot" data-progress={progress} aria-hidden="true"/>{say(w.heldChip, {count})}
  </span>
}

export type HeldCount = {count: number; progress: BotRequestProgress}

/** The roster key of one Bot's held count: its machine and Bot id. */
export const heldKey = (machine: string, bot: string) => JSON.stringify([machine, bot])

/**
 * Open cards held per Bot across the sources, for the roster's chips. A
 * source whose read fails keeps its last counts out of sight rather than
 * showing old numbers as current.
 */
export function useHeldCounts(sources: readonly SourceClient[]): ReadonlyMap<string, HeldCount> {
  const [counts, setCounts] = useState<Record<string, ReadonlyMap<string, HeldCount>>>({})
  const [tick, setTick] = useState(0)
  const identity = sources.map(source => source.machineID + "|" + source.authorizationRevision).join(",")
  useEffect(() => {
    const control = new AbortController()
    for (const source of sources) {
      const fetcher = botRequestFetcher(source)
      if (!fetcher) continue
      void readBotRequests({state: "open"}, control.signal, fetcher).then(
        cards => { if (!control.signal.aborted) setCounts(old => ({...old, [source.machineID]: heldCounts(cards ?? [])})) },
        () => { if (!control.signal.aborted) setCounts(old => { const next = {...old}; delete next[source.machineID]; return next }) },
      )
    }
    return () => control.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [identity, tick])
  useEffect(() => {
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") setTick(value => value + 1) }, 30_000)
    return () => window.clearInterval(timer)
  }, [])
  const out = new Map<string, HeldCount>()
  for (const source of sources) for (const [bot, count] of counts[source.machineID] ?? []) out.set(heldKey(source.machineID, bot), count)
  return out
}

const HeldContext = createContext<ReadonlyMap<string, HeldCount>>(new Map())

/** Reads the held counts once for every roster row below it. */
export function HeldCountsScope({sources, children}: {sources: readonly SourceClient[]; children: ReactNode}) {
  return <HeldContext.Provider value={useHeldCounts(sources)}>{children}</HeldContext.Provider>
}

/** One Bot's held count, from the nearest HeldCountsScope. */
export function useHeld(machine: string, bot: string): HeldCount | undefined {
  return useContext(HeldContext).get(heldKey(machine, bot))
}

/** The card reads and writes bound to the source that reaches the Bot's machine; undefined where it has none. */
export function useBotRequestIO(source: SourceClient) {
  return useMemo(() => {
    const fetcher = botRequestFetcher(source)
    return fetcher && {
      list: (q: {bot?: string; state?: string}, s?: AbortSignal) => readBotRequests(q, s, fetcher),
      read: (id: string, s?: AbortSignal) => readBotRequest(id, s, fetcher),
      write: (id: string, a: Parameters<typeof writeBotRequest>[1], body: Parameters<typeof writeBotRequest>[2], k: string, s?: AbortSignal) => writeBotRequest(id, a, body, k, s, fetcher),
    }
  }, [source])
}

/**
 * The held cards line as a Bot's chat draws it: writes are offered where the
 * chat may send and the source may carry them, and Ask again fills the
 * composer for the person to send.
 */
export function ChatHeldCards({source, enabled, cards, bot, botName, bots, botDirectoryState, w, onCompose, onOpen}: {
  source: SourceClient; enabled: boolean; cards: readonly W.BotRequestView[]; bot: string; botName: string; bots: readonly W.ProjectAssistantBot[]; w: Words
  botDirectoryState: BotDirectoryState
  onCompose: (text: string) => void; onOpen: (card: W.BotRequestView, mode: "view" | "handoff" | "complete") => void
}) {
  const local = source.authorizationRevision === "local"
  return <HeldCards cards={cards} bot={bot} botName={botName} bots={bots} botDirectoryState={botDirectoryState} w={w} writable={enabled && (local || source.canWriteBotRequests?.() === true)}
    onOpenBoard={() => { window.location.hash = "page=work" }}
    onAction={(card, action) => action === "retry" ? onCompose(say(w.heldRetryText, {title: card.title, id: card.id})) : onOpen(card, action)}/>
}
