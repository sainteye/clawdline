import type * as W from "@clawdline/contract"
import type {Words} from "./words"

/**
 * Bot request cards as the console reads them (docs/bot-request-cards.md §9).
 * Everything shown comes from GET /v1/bot-request/cards; nothing here guesses
 * a state the daemon did not answer.
 */

export type BotRequestProgress = "in_progress" | "waiting" | "done" | "cancelled" | "merged"

/** The card's light. An unknown word from a newer daemon falls back to the card's own state. */
export function progressOf(card: Pick<W.BotRequestView, "progress_state" | "state" | "waiting">): BotRequestProgress {
  switch (card.progress_state) {
    case "waiting_for_you": return "waiting"
    case "in_progress": return "in_progress"
    case "done": return "done"
    case "cancelled": return "cancelled"
    case "merged": return "merged"
  }
  if (card.state === "done" || card.state === "cancelled" || card.state === "merged") return card.state
  return card.waiting ? "waiting" : "in_progress"
}

export function progressLabel(progress: BotRequestProgress, w: Words): string {
  return {in_progress: w.requestStateInProgress, waiting: w.requestStateWaiting, done: w.requestStateDone,
    cancelled: w.requestStateCancelled, merged: w.requestStateMerged}[progress]
}

/** "n/m ✓", or nothing for a card without a checklist: 0/0 would read as no progress. */
export function checklistProgress(card: Pick<W.BotRequestView, "done" | "total">, w: Words): string {
  if (!(card.total > 0)) return ""
  return w.requestProgress.replace("{done}", String(card.done)).replace("{total}", String(card.total))
}

const rank: Record<BotRequestProgress, number> = {waiting: 0, in_progress: 1, done: 2, cancelled: 2, merged: 2}

/** Cards waiting for the person first, then the ones in progress, then closed ones; newest update first within each. */
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

/** `person`, `bot:<id>` or a bare Bot id, as a name the person can read. */
export function actorName(actor: string | undefined, bots: readonly W.ProjectAssistantBot[], w: Words): string {
  if (!actor) return w.requestUnknownBot
  if (actor === "person") return w.requestYou
  const id = actor.startsWith("bot:") ? actor.slice(4) : actor
  return botOf(id, bots)?.name || w.requestUnknownBot
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
    case "bot_request_full": return w.requestRefusedFull
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
  const params = new URLSearchParams()
  if (query.bot) params.set("bot", query.bot)
  if (query.state) params.set("state", query.state)
  const res = await fetcher("/v1/bot-request/cards" + (params.size ? "?" + params : ""), {credentials: "same-origin", signal})
  if (res.status === 404 || res.status === 405 || res.status === 501) return null
  const body = await answer(res)
  if (!res.ok) throw refused(body, res.status)
  const cards = (body as Partial<W.BotRequestPage> | undefined)?.cards
  return Array.isArray(cards) ? cards : null
}

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
    res = await fetcher(`/v1/bot-request/cards/${encodeURIComponent(id)}/${action}`, {method: "POST", credentials: "same-origin", signal,
      headers: {"Content-Type": "application/json", "Idempotency-Key": key}, body: JSON.stringify(write)})
  } catch {
    throw new BotRequestRefused("unconfirmed")
  }
  const body = await answer(res)
  if (!res.ok || !(body as W.BotRequestWriteResult | undefined)?.card) throw refused(body, res.status)
  return body as W.BotRequestWriteResult
}

/** Cards are read from the local daemon only; a machine reached through Cloud has no wire words for them yet. */
export function readsBotRequests(source: {authorizationRevision: string} | undefined): boolean {
  return source?.authorizationRevision === "local"
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
