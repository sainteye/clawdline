/*
 * Cards that outlive the page they were sent from (F4).
 *
 * A card is the only copy of what somebody wrote: the box empties at the press
 * (`Composer.tsx`) and nothing puts the words back. It is also the only copy of
 * the card's `request`, and that is what stops the same message being typed
 * twice — every attempt of a card goes under it as the Idempotency-Key, and the
 * Mac answers a second attempt with the first one's answer rather than typing
 * the words again (F2). So a card that lives only in `PendingSends` takes both
 * with it when the page goes: a reload, a sign-in that navigates away, the
 * machine switch that reloads on purpose (`cloud/CloudGate.tsx`), or an iPhone
 * taking a Home Screen app back. Whoever writes the message again writes a
 * **new** request, which no receipt on the Mac knows anything about, so F2 is
 * walked around rather than broken.
 *
 * This file keeps the cards between page loads, and the one thing it must not
 * do is send anything. Nothing here posts; a restored card is put back on the
 * page and the transcript is read as it always is, which either shows the turn
 * — and the card goes, as any settled card does — or does not, and the card
 * says it does not know (F3). Sending again stays a person's press.
 *
 * **It is this browser's copy and nothing else.** `localStorage` is per origin
 * and per browser profile: another device, another browser, or a cleared site
 * has none of it, and nothing here is sent to the daemon or to Clawdline Cloud.
 * The card's words say "this page", never "your messages", because a card that
 * claimed to follow somebody between devices would be lying.
 *
 * `sessionStorage` is the smaller answer and was not taken: it is per tab and
 * dies with it, which leaves out two of the four ways a card is lost — the tab
 * closed, and the Home Screen app the system took back.
 *
 * Nothing here is imported at run time, so `node --test` loads it as it is; the
 * browser's `localStorage` is handed in by `send.ts`.
 */
import type { PendingSend } from "./pending.js"

/** Where the cards are kept between page loads. Either call may throw; neither is needed for the page to work. */
export interface CardStore {
  read(): string | null
  write(value: string): void
}

export interface CardsDeps {
  /** This browser's `localStorage`, or null where there is none to keep cards in. */
  store: CardStore | null
  /** `pending.ts` `turnWords`, handed in so that this file imports nothing at run time. */
  turnWords(text: string, imageCount: number): string
}

/**
 * The code a card carries when the page went away while it was being sent.
 * Nothing this end heard says whether the words reached the terminal — the
 * request died with the page, not with an answer — so the card is `unknown`,
 * which is the state that offers a look rather than "try again" (F3).
 */
export const INTERRUPTED = "interrupted"

/**
 * How long a card is kept. The same twenty-four hours as the Mac's receipt
 * window (`store/receipts.go` `ReceiptWindow`), and for its sake: past it the
 * receipt for the card's request has gone, so sending the card again is a
 * message the Mac cannot recognise — which is the thing this file exists to
 * prevent. A card older than its receipt is not worth keeping.
 */
export const KEPT_MS = 24 * 60 * 60 * 1000

/** The most cards kept; the oldest go first. A page with more than this on it is not a page anybody is reading. */
export const KEPT_CARDS = 20

/** The most tokens remembered as let go, so another tab cannot put one back. */
export const KEPT_LET_GO = 64

/**
 * The most characters the whole record may take. `localStorage` is about five
 * megabytes for the whole origin — shared with the bar's history and the
 * list's clock — and WebKit counts it in UTF-16 units, so this is about a
 * megabyte of it. A picture is a `data:` URL of up to five megabytes on its
 * own (`legacy/shots-bridge.ts` `MAX_EACH`), so pictures are what goes first.
 */
export const KEPT_CHARS = 512 * 1024

/** One card as it is kept: the fields a restored card needs, and no others. */
interface Kept {
  t: string
  s: string
  x: string
  /** The pictures. A card kept without them has `""` in each one's place, which keeps the count right; see `partial`. */
  p: string[]
  r: string
  /** `PendingState`, as it was. */
  st: string
  f: string
  at: number
  ac: number
  /** `known`, as `[key, count]` pairs, already cut to the keys that can matter. */
  k: [string, number][]
  /** 1 when the pictures were dropped to fit. */
  q?: 1
  /** Which daemon the card was written for; absent for the one that served the page. */
  m?: string
}

interface Record_ {
  v: 1
  cards: Kept[]
  /** Tokens this browser has finished with, newest last, as `[token, when]`. */
  gone: [string, number][]
}

const empty = (): Record_ => ({ v: 1, cards: [], gone: [] })

/**
 * The entries of `known` that can still decide anything for this card.
 *
 * `reconcile` only ever asks `known` about a turn whose words already equal the
 * card's, so an entry under any other words is weight and nothing else — and
 * `known` is read from a two-hundred-entry transcript, which would otherwise be
 * most of what is kept.
 */
function knownForCard(card: PendingSend, want: string): [string, number][] {
  const kept: [string, number][] = []
  for (const [key, count] of card.known) {
    const cut = key.indexOf("\u0001")
    if (cut >= 0 && key.slice(cut + 1) === want) kept.push([key, count])
  }
  return kept
}

function keptOf(card: PendingSend, want: string, daemon: string): Kept {
  const kept: Kept = {
    t: card.token,
    s: card.session,
    x: card.text,
    p: card.partial ? card.pictures.map(() => "") : [...card.pictures],
    r: card.request,
    st: card.state,
    f: card.failure,
    at: card.sentAt,
    ac: card.acceptedAt,
    k: knownForCard(card, want),
  }
  if (card.partial) kept.q = 1
  if (daemon) kept.m = daemon
  return kept
}

/** A card as it comes back. What it may say about itself is decided here, not by what it said before the page went. */
function cardOf(kept: Kept): PendingSend | null {
  if (typeof kept.t !== "string" || !kept.t) return null
  if (typeof kept.s !== "string" || typeof kept.x !== "string" || typeof kept.r !== "string") return null
  if (!Array.isArray(kept.p) || !kept.p.every((url) => typeof url === "string")) return null
  if (typeof kept.at !== "number") return null
  const partial = kept.q === 1
  // An accepted card was answered: the daemon said the bytes reached the
  // terminal, and that answer is still good. A card still sending was not
  // answered at all, and now never will be — its request went with the page —
  // so it is the unknown of F3 rather than a failure.
  const was = kept.st
  const state = was === "accepted" ? "accepted" : was === "failed" ? "failed" : was === "unknown" ? "unknown" : "unknown"
  const failure = was === "sending" || was === "accepted" ? INTERRUPTED : typeof kept.f === "string" ? kept.f : INTERRUPTED
  const known = new Map<string, number>()
  if (Array.isArray(kept.k)) {
    for (const pair of kept.k) {
      if (Array.isArray(pair) && typeof pair[0] === "string" && typeof pair[1] === "number") known.set(pair[0], pair[1])
    }
  }
  return {
    token: kept.t,
    session: kept.s,
    text: kept.x,
    pictures: kept.p,
    request: kept.r,
    state: state as PendingSend["state"],
    failure: state === "accepted" ? "" : failure,
    // What an earlier read of the transcript said is not what this page has
    // read. A card comes back not knowing, and one fresh look — a person's
    // press, one request — is what may say otherwise.
    absent: false,
    sentAt: kept.at,
    acceptedAt: typeof kept.ac === "number" ? kept.ac : 0,
    known,
    checking: false,
    partial,
  }
}

function parse(raw: string | null): Record_ {
  if (!raw) return empty()
  try {
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== "object") return empty()
    const held = value as { v?: unknown; cards?: unknown; gone?: unknown }
    if (held.v !== 1) return empty()
    const cards = Array.isArray(held.cards) ? (held.cards as Kept[]) : []
    const gone: [string, number][] = []
    if (Array.isArray(held.gone)) {
      for (const pair of held.gone) {
        if (Array.isArray(pair) && typeof pair[0] === "string" && typeof pair[1] === "number") gone.push([pair[0], pair[1]])
      }
    }
    return { v: 1, cards, gone }
  } catch {
    // Somebody else's value, or a half-written one: no cards is a working page.
    return empty()
  }
}

/**
 * The record, cut to what may be kept: nothing older than `KEPT_MS`, no more
 * than `KEPT_CARDS` of them, and under `KEPT_CHARS` once written out.
 *
 * Pictures go before words do. A card whose pictures were dropped keeps a `""`
 * in each one's place, so that it still counts as a message with pictures —
 * which is how the transcript settles it — and is marked `q`, which is what
 * stops it being sent again: a second attempt under the same request with a
 * different body is refused (`idempotency_key_reused`), and a new request would
 * be the message twice.
 */
function shed(record: Record_, now: number): Record_ {
  let cards = record.cards.filter((card) => typeof card?.at === "number" && now - card.at < KEPT_MS)
  cards.sort((a, b) => a.at - b.at)
  if (cards.length > KEPT_CARDS) cards = cards.slice(cards.length - KEPT_CARDS)
  const gone = record.gone.filter(([, at]) => now - at < KEPT_MS).slice(-KEPT_LET_GO)
  const size = () => JSON.stringify({ v: 1, cards, gone }).length
  for (let i = 0; i < cards.length && size() > KEPT_CHARS; i++) {
    if (!cards[i].p.some((url) => url !== "")) continue
    cards[i] = { ...cards[i], p: cards[i].p.map(() => ""), q: 1 }
  }
  while (cards.length && size() > KEPT_CHARS) cards = cards.slice(1)
  return { v: 1, cards, gone }
}

/**
 * This browser's kept cards, and what it has finished with.
 *
 * One of these belongs to one page. It knows which tokens that page holds, so
 * that another tab's cards in the same `localStorage` are carried rather than
 * overwritten, and a card either tab has let go — dismissed, or settled by the
 * transcript — stays gone instead of being written back by whichever tab still
 * had it.
 *
 * **A card is put back only for the daemon it was written for.** The daemon's
 * own console is one daemon per origin, so there is nothing to tell apart; the
 * hosted console is every machine on the account under one origin, and a
 * session id is a terminal id — a tmux pane is `%1` on every Mac that has one.
 * A card kept while one machine was chosen and put back while another is would
 * be words addressed to whatever that other machine's `%1` holds, which is the
 * mistake `relay-writer.ts` `identity()` refuses to make with a row that has
 * gone (F5). So `restore` is told which daemon this page ended up talking to
 * and answers for that one; the others' cards are carried untouched.
 */
export class Cards {
  private readonly deps: CardsDeps
  /** Tokens this page has held since it loaded. */
  private held = new Set<string>()
  /** The daemon this page is talking to, once it is known; null before that, and then nothing is kept. */
  private daemon: string | null = null

  constructor(deps: CardsDeps) {
    this.deps = deps
  }

  private get store(): CardStore | null {
    return this.deps.store
  }

  /**
   * The cards to put back on the page, for the daemon this page talks to: the
   * empty string for the one that served it, a machine id for the hosted
   * console. Reading is allowed to fail; then there are none.
   */
  restore(daemon: string, now: number): PendingSend[] {
    this.daemon = daemon
    let raw: string | null = null
    try {
      raw = this.store?.read() ?? null
    } catch {
      // A private window, or site data this browser will not give out.
      return []
    }
    const record = shed(parse(raw), now)
    const gone = new Set(record.gone.map(([token]) => token))
    const cards: PendingSend[] = []
    for (const kept of record.cards) {
      if (gone.has(kept?.t)) continue
      if ((kept?.m ?? "") !== daemon) continue
      const card = cardOf(kept)
      if (card) cards.push(card)
    }
    for (const card of cards) this.held.add(card.token)
    return cards
  }

  /**
   * Keep what the page holds now. Called after every change to the cards, which
   * is a few times a message: the write is small and the alternative is a card
   * that was only kept some of the time.
   */
  keep(cards: readonly PendingSend[], now: number): void {
    // Before this page knows which daemon it is talking to there is nothing on
    // it to keep, and a card written under the wrong name is worse than none.
    if (!this.store || this.daemon === null) return
    let raw: string | null = null
    try {
      raw = this.store.read()
    } catch {
      raw = null
    }
    const record = parse(raw)
    const mine = new Set(cards.map((card) => card.token))
    const gone = new Map(record.gone)
    // A token this page held and holds no longer is finished with: dismissed,
    // or settled by a turn in the transcript. Either way it must not come back
    // from another tab's copy.
    for (const token of this.held) if (!mine.has(token)) gone.set(token, gone.get(token) ?? now)
    this.held = mine
    const others = record.cards.filter((kept) => kept && !mine.has(kept.t) && !gone.has(kept.t))
    const daemon = this.daemon
    const ours = cards.map((card) => keptOf(card, this.deps.turnWords(card.text, card.pictures.length), daemon))
    const next = shed({ v: 1, cards: [...others, ...ours], gone: [...gone] }, now)
    try {
      this.store.write(JSON.stringify(next))
    } catch {
      // No room, or a private window. The page still works; the cards on it
      // are back to living only as long as it does.
    }
  }

  /** Tokens another tab has finished with, as its write left them. */
  letGo(raw: string | null, now: number): string[] {
    const record = shed(parse(raw), now)
    return record.gone.filter(([token]) => this.held.has(token)).map(([token]) => token)
  }
}
