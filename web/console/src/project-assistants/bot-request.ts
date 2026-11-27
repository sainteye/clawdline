import type * as W from "@clawdline/contract"
import type {BotDirectoryState} from "./open-bot"
import type {Words} from "./words"

/**
 * Bot request cards as the console reads them (docs/bot-request-cards.md §9).
 * Everything shown comes from GET /v1/bot-request/cards; nothing here guesses
 * a state the daemon did not answer.
 */

export type BotRequestProgress = "stalled" | "in_progress" | "waiting" | "done" | "cancelled" | "merged"

/** The card's light. An unknown word from a newer daemon falls back to the card's own state. */
export function progressOf(card: Pick<W.BotRequestView, "progress_state" | "state" | "waiting">): BotRequestProgress {
  switch (card.progress_state) {
    case "waiting_for_you": return "waiting"
    case "stalled": return "stalled"
    case "in_progress": return "in_progress"
    case "done": return "done"
    case "cancelled": return "cancelled"
    case "merged": return "merged"
  }
  if (card.state === "done" || card.state === "cancelled" || card.state === "merged") return card.state
  return card.waiting ? "waiting" : "in_progress"
}

export function progressLabel(progress: BotRequestProgress, w: Words): string {
  return {stalled: w.requestStateStalled, in_progress: w.requestStateInProgress, waiting: w.requestStateWaiting, done: w.requestStateDone,
    cancelled: w.requestStateCancelled, merged: w.requestStateMerged}[progress]
}

/** "n/m ✓", or nothing for a card without a checklist: 0/0 would read as no progress. */
export function checklistProgress(card: Pick<W.BotRequestView, "done" | "total">, w: Words): string {
  if (!(card.total > 0)) return ""
  return w.requestProgress.replace("{done}", String(card.done)).replace("{total}", String(card.total))
}

const rank: Record<BotRequestProgress, number> = {stalled: 0, waiting: 1, in_progress: 2, done: 3, cancelled: 3, merged: 3}

/** Stalled cards first, then those waiting for the person, then the ones in progress, then closed ones; newest update first within each. */
export function sortBotRequests(cards: readonly W.BotRequestView[]): W.BotRequestView[] {
  const at = (card: W.BotRequestView) => { const t = Date.parse(card.updated_at); return Number.isFinite(t) ? t : 0 }
  return [...cards].sort((a, b) => rank[progressOf(a)] - rank[progressOf(b)] || at(b) - at(a) || a.id.localeCompare(b.id))
}

/** "Updated 5 min ago". An unreadable time says nothing instead of "just now". */
export function lastUpdated(updatedAt: string, now: number, w: Words): string {
  const at = Date.parse(updatedAt)
  if (!Number.isFinite(at)) return ""
  const minutes = Math.floor(Math.max(0, now - at) / 60_000)
  if (minutes < 1) return w.requestUpdatedNow
  const span = minutes < 60 ? w.requestMinutes.replace("{count}", String(minutes))
    : minutes < 60 * 24 ? w.requestHours.replace("{count}", String(Math.floor(minutes / 60)))
      : w.requestDays.replace("{count}", String(Math.floor(minutes / (60 * 24))))
  return w.requestUpdated.replace("{time}", span)
}

export type BotRequestHandoff = {id: number; from: string; to?: string; at: string; note: string}

/**
 * One line per handoff, oldest first. The history records who handed the card
 * on and the note, not the receiver. The receiver is named only when the chain
 * alone settles it: every Bot after the first arrived by exactly one handoff.
 */
export function handoffHistory(card: Pick<W.BotRequestView, "events" | "chain">): BotRequestHandoff[] {
  const events = card.events ?? []
  const handed = events.filter(event => event.kind === "baton.handed")
  const certain = !events.some(event => event.kind === "chain.joined") && handed.length === card.chain.length - 1
  return handed.map((event, index) => ({id: event.id, from: event.actor, to: certain ? card.chain[index + 1] : undefined,
    at: event.at, note: event.detail ?? ""}))
}

export function requestBotName(bot: W.ProjectAssistantBot | undefined, w: Words, state: BotDirectoryState): string {
  if (bot?.name) return bot.name
  if (state === "loading") return "…"
  return state === "ready" ? w.requestUnknownBot : w.unavailable
}

/** `person`, `bot:<id>` or a bare Bot id, as a name the person can read. */
export function actorName(actor: string | undefined, bots: readonly W.ProjectAssistantBot[], w: Words, state: BotDirectoryState = "ready"): string {
  if (actor === "person") return w.requestYou
  const id = actor?.startsWith("bot:") ? actor.slice(4) : actor
  return requestBotName(id ? botOf(id, bots) : undefined, w, state)
}

export function botOf(id: string, bots: readonly W.ProjectAssistantBot[]): W.ProjectAssistantBot | undefined {
  return bots.find(bot => bot.key.bot_id === id)
}

export type NotePart = {text: string; href?: string}

/** A note's http(s) addresses become links; everything else stays text. */
export function noteParts(note: string): NotePart[] {
  const parts: NotePart[] = []
  let last = 0
  for (const match of note.matchAll(/https?:\/\/[^\s<>"']+/gu)) {
    const raw = match[0].replace(/[.,;:!?)\]}\uff0c\u3002\uff1b\uff1a\uff01\uff1f\uff09\u300d\u300f]+$/u, "")
    let href = ""
    try { href = new URL(raw).href } catch { continue }
    if (match.index > last) parts.push({text: note.slice(last, match.index)})
    parts.push({text: raw, href})
    last = match.index + raw.length
  }
  if (last < note.length) parts.push({text: note.slice(last)})
  return parts
}

/** The work link's card, which the daemon sends although the generated link type does not name it yet. */
export function linkRequestID(link: W.ProjectAssistantWorkLink): string {
  const id = (link as {bot_request_id?: unknown}).bot_request_id
  return typeof id === "string" ? id : ""
}

export type BotRequestPlacement = {
  /** Work link id → the card shown above it: the first link of each card. */
  above: Map<string, W.BotRequestView>
  /** Card id → its dispatched Sessions loaded in this chat. */
  links: Map<string, W.ProjectAssistantWorkLink[]>
  /** Open cards with no work result card in this chat, shown at its end. */
  loose: W.BotRequestView[]
}

/** Where each card sits in a Bot's chat. A chat without cards gets nothing, so it renders as before. */
export function placeBotRequests(cards: readonly W.BotRequestView[], links: readonly W.ProjectAssistantWorkLink[]): BotRequestPlacement {
  const byID = new Map(cards.map(card => [card.id, card]))
  const above = new Map<string, W.BotRequestView>(), grouped = new Map<string, W.ProjectAssistantWorkLink[]>()
  for (const link of links) {
    const card = byID.get(linkRequestID(link))
    if (!card) continue
    const list = grouped.get(card.id)
    if (list) { if (!list.some(known => known.id === link.id)) list.push(link) }
    else { grouped.set(card.id, [link]); above.set(link.id, card) }
  }
  const loose = sortBotRequests(cards.filter(card => !grouped.has(card.id) && card.state === "open"))
  return {above, links: grouped, loose}
}

export type BotRequestAction = "handoff" | "check" | "complete" | "cancel" | "reopen"

/** A typed refusal of a card read or write. `unconfirmed` when no answer came back. */
export class BotRequestRefused extends Error {
  readonly code: string
  readonly field: string
  readonly version?: number
  constructor(code: string, field = "", version?: number) {
    super(code)
    this.code = code; this.field = field; this.version = version
  }
}

/** A refusal in plain words. */
export function refusalWords(code: string, w: Words): string {
  switch (code) {
    case "bot_request_checklist_open": return w.requestRefusedOpenEntries
    case "bot_request_version_conflict": return w.requestRefusedConflict
    case "bot_request_closed": return w.requestRefusedClosed
    case "bot_request_already_merged": return w.requestRefusedMerged
    case "bot_request_forbidden": return w.requestRefusedForbidden
    case "bot_request_not_found": return w.requestRefusedNotFound
    case "bot_request_invalid": return w.requestRefusedInvalid
    case "unconfirmed": return w.requestUnconfirmed
  }
  return w.requestRefusedOther
}

type Fetcher = (input: string, init?: RequestInit) => Promise<Response>

async function answer(res: Response): Promise<unknown> {
  try { return JSON.parse(await res.text()) as unknown } catch { return undefined }
}

function refused(body: unknown, status: number): BotRequestRefused {
  const refusal = body as Partial<W.BotRequestRefusal> | undefined
  return new BotRequestRefused(typeof refusal?.error === "string" ? refusal.error : `http_${status}`,
    typeof refusal?.detail === "string" ? refusal.detail : "", typeof refusal?.version === "number" ? refusal.version : undefined)
}

/**
 * The cards, or null when this origin has no such route: an older daemon
 * (404), or a hosted console that answers the path with its own page. Null
 * hides the feature; a failure that is not "no route" throws, so "could not
 * load" is never shown as "nothing here".
 */
export async function readBotRequests(query: {bot?: string; state?: string}, signal?: AbortSignal, fetcher: Fetcher = fetch): Promise<W.BotRequestView[] | null> {
  const out: W.BotRequestView[] = []
  const seen = new Set<string>()
  let cursor = ""
  // Every page is read: a list cut at the first page would show fewer cards
  // with nothing to say so. A cursor that repeats or never ends is an error.
  for (let page = 0; page < BOT_REQUEST_MAX_PAGES; page++) {
    const params = new URLSearchParams()
    if (query.bot) params.set("bot", query.bot)
    if (query.state) params.set("state", query.state)
    if (cursor) params.set("cursor", cursor)
    const res = await fetcher("/v1/bot-request/cards" + (params.size ? "?" + params : ""), {credentials: "same-origin", signal})
    if (page === 0 && (res.status === 404 || res.status === 405 || res.status === 501)) return null
    const body = await answer(res)
    if (!res.ok) throw refused(body, res.status)
    const reply = body as Partial<W.BotRequestPage> | undefined
    if (!Array.isArray(reply?.cards)) {
      if (page === 0) return null
      throw new BotRequestRefused("unconfirmed", "bot_request_page")
    }
    out.push(...reply.cards)
    const next = typeof reply.next_cursor === "string" ? reply.next_cursor : ""
    if (!next) return out
    if (seen.has(next)) throw new BotRequestRefused("unconfirmed", "bot_request_page")
    seen.add(next)
    cursor = next
  }
  throw new BotRequestRefused("unconfirmed", "bot_request_page")
}

/** Pages one card list may span (100 cards each) before the read is an error. */
export const BOT_REQUEST_MAX_PAGES = 50

/** One card with its history. */
export async function readBotRequest(id: string, signal?: AbortSignal, fetcher: Fetcher = fetch): Promise<W.BotRequestView> {
  const res = await fetcher("/v1/bot-request/cards/" + encodeURIComponent(id), {credentials: "same-origin", signal})
  const body = await answer(res)
  if (!res.ok || !body || typeof (body as W.BotRequestView).id !== "string") throw refused(body, res.status)
  return body as W.BotRequestView
}

/** The person's write. `key` is minted once per decision so a retry answers the first attempt. */
export async function writeBotRequest(id: string, action: BotRequestAction, write: W.BotRequestPersonWrite, key: string,
  signal?: AbortSignal, fetcher: Fetcher = fetch): Promise<W.BotRequestWriteResult> {
  let res: Response
  try {
    const body = Object.fromEntries(Object.entries({...write, idempotency_key: key}).filter(([, value]) => value !== "" && value !== undefined))
    res = await fetcher(`/v1/bot-request/cards/${encodeURIComponent(id)}/${action}`, {method: "POST", credentials: "same-origin", signal,
      headers: {"Content-Type": "application/json", "Idempotency-Key": key}, body: JSON.stringify(body)})
  } catch {
    throw new BotRequestRefused("unconfirmed")
  }
  const body = await answer(res)
  if (!res.ok || !(body as W.BotRequestWriteResult | undefined)?.card) throw refused(body, res.status)
  return body as W.BotRequestWriteResult
}

type CardSource = {authorizationRevision: string; canBotRequests?(): boolean; botRequestFetch?(): Fetcher}

/** Cards are read from the Bot's own machine: this daemon, or a machine through Cloud from API 130. */
export function readsBotRequests(source: CardSource | undefined): boolean {
  return source?.authorizationRevision === "local" || source?.canBotRequests?.() === true
}

/** The fetch that reaches the source's machine for the card routes, or undefined when it has none. */
export function botRequestFetcher(source: CardSource | undefined): Fetcher | undefined {
  if (source?.canBotRequests?.() === true && source.botRequestFetch) return source.botRequestFetch()
  return source?.authorizationRevision === "local" ? (input, init) => fetch(input, init) : undefined
}

/** The card reads and writes through one fetch: the page's own when none is given. */
export function boundBotRequests(fetcher?: Fetcher) {
  return {
    list: (q: {bot?: string; state?: string}, s?: AbortSignal) => readBotRequests(q, s, fetcher),
    read: (id: string, s?: AbortSignal) => readBotRequest(id, s, fetcher),
    write: (id: string, a: BotRequestAction, body: W.BotRequestPersonWrite, k: string, s?: AbortSignal) => writeBotRequest(id, a, body, k, s, fetcher),
  }
}

/** Why a stalled card stopped, in the person's words. */
export function stalledWords(card: Pick<W.BotRequestView, "stalled_reason">, w: Words): string {
  switch (card.stalled_reason) {
    case "work_failed": return w.requestStalledWork
    case "turn_unsettled": return w.requestStalledTurn
    case "turn_and_work": return w.requestStalledTurnAndWork
  }
  return w.requestStalledOther
}

/** The open cards a Bot holds, stalled first, and how many it has closed as done. */
export function heldCards(cards: readonly W.BotRequestView[], bot: string): {open: W.BotRequestView[]; done: W.BotRequestView[]} {
  const held = cards.filter(card => card.baton.bot === bot)
  return {open: sortBotRequests(held.filter(card => card.state === "open")), done: sortBotRequests(held.filter(card => card.state === "done"))}
}

/** Open cards held per Bot id, for the roster's chip. */
export function heldCounts(cards: readonly W.BotRequestView[]): Map<string, {count: number; progress: BotRequestProgress}> {
  const out = new Map<string, {count: number; progress: BotRequestProgress}>()
  for (const card of cards) {
    if (card.state !== "open" || !card.baton.bot) continue
    const progress = progressOf(card), known = out.get(card.baton.bot)
    out.set(card.baton.bot, {count: (known?.count ?? 0) + 1, progress: known && rank[known.progress] <= rank[progress] ? known.progress : progress})
  }
  return out
}

/** Who gave the holder the card: the Bot before it in the chain, or its creator. */
export function cardFrom(card: Pick<W.BotRequestView, "chain" | "created_by" | "baton">): {actor: string; handed: boolean} {
  const at = card.chain.lastIndexOf(card.baton.bot)
  if (at > 0) return {actor: card.chain[at - 1], handed: true}
  return {actor: card.created_by, handed: false}
}

export const BOT_WORK_SHOWN_KEY = "clawdline.board.bot-work"

type ShownStore = Pick<Storage, "getItem" | "setItem">
function browserStore(): ShownStore | null {
  try { return typeof localStorage === "undefined" ? null : localStorage } catch { return null }
}

/** Shown unless this browser chose to hide it; blocked storage reads as shown. */
export function readBotWorkShown(store: ShownStore | null = browserStore()): boolean {
  try { return store?.getItem(BOT_WORK_SHOWN_KEY) !== "hidden" } catch { return true }
}

export function writeBotWorkShown(shown: boolean, store: ShownStore | null = browserStore()): void {
  try { store?.setItem(BOT_WORK_SHOWN_KEY, shown ? "shown" : "hidden") } catch { /* the choice lasts for this page only */ }
}
