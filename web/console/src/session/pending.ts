/*
 * Messages this page has sent and not yet seen in the conversation.
 *
 * `view/waits.js` `Optimistic` and `view/optimistic-data.js`, for this daemon.
 * The card is the Swift app's pending turn — `.entry.pending`, drawn by
 * `Transcript.tsx` at the newest end of the transcript, which is the end just
 * above the box — and it goes the moment the turn it stands for is read back.
 * Three things differ, each because of what this daemon answers:
 *
 * - **The card is there from the press**, not from the answer. The Swift app
 *   had the Send button's sweep for the round trip and added its card when the
 *   Mac answered. Here the round trip is short and the wait left bare is the one
 *   after it, until the transcript is read again, so one card covers both:
 *   "sending", then the catalog's "the machine has it".
 * - **A send that fails keeps its card**, saying so, with the words still on it
 *   and a way to send them again. The Swift app toasted and the words were
 *   gone. They are still not put back in the box (see `Composer.tsx`): a send
 *   that fails at this end may have been delivered, and if it was, the
 *   transcript shows it and the card goes like any other.
 * - **Nothing on the answer names the request**, so a card is settled the way
 *   the Swift app settles one by its words: a user turn with the same words,
 *   written no earlier than a little before the send, that was not already in
 *   the transcript when the send began, one turn per card. `ActionResult` is
 *   `ok` and nothing else.
 *
 * **A card outlives the page.** The words and, more to the point, the card's
 * one `request` are kept in this browser's store and put back by `restore`
 * (`persist.ts`, F4) — because a card that went with the page took the request
 * with it, and the same message written again under a new request is the thing
 * the request exists to stop (F2). Nothing is sent on the way back: a restored
 * card that was still sending is `unknown`, and the transcript, read as it
 * always is, either shows the turn and takes the card with it or does not.
 *
 * Nothing here is imported at run time, so `node --test` loads it as it is;
 * the part that talks to the daemon is `send.ts`.
 */

/**
 * Where a card has got to. `failed` is a refusal that proves nothing was typed;
 * `unknown` is every other failure — no answer, an answer lost on the way back,
 * a terminal that failed part-way — after which the words may be on the Mac,
 * and the card says it does not know rather than that it failed (F3).
 */
export type PendingState = "sending" | "accepted" | "failed" | "unknown"

export interface PendingSend {
  readonly token: string
  readonly session: string
  readonly text: string
  /** The pictures as `data:` URLs, kept so that sending again sends them too. */
  readonly pictures: readonly string[]
  /**
   * The one request every attempt of this card is sent under, as its
   * Idempotency-Key (F2). The Mac answers a second attempt with the first
   * one's answer rather than typing the words again, so "try again" after an
   * answer that was lost is not the same message twice.
   */
  readonly request: string
  state: PendingState
  /** The refusal's code, while `failed` or `unknown`. */
  failure: string
  /**
   * An `unknown` card whose transcript was read, fresh, after it failed, and
   * did not hold the turn. Only then is sending it again offered — under the
   * same request, so an attempt still on its way is not doubled.
   */
  absent: boolean
  /** The last attempt, in milliseconds. */
  sentAt: number
  /** When the daemon said yes, in milliseconds; 0 before. */
  acceptedAt: number
  /** Turns that were already in the transcript when the attempt began, counted by `occurrenceKey`. */
  known: Map<string, number>
  /**
   * "Try again" was pressed and the page is first reading the transcript, in
   * case the attempt that failed arrived after all (`send.ts` `resend`). The
   * card says it is sending; `known` is still the failed attempt's.
   */
  checking: boolean
  /**
   * The card came back from this browser's store (`persist.ts`, F4) without
   * its pictures, which did not fit in what may be kept. Each one is an empty
   * string, so the card still counts as a message with pictures and is settled
   * by its turn like any other, and the words are still here to read and copy —
   * but it is never sent again: a second attempt under the same request with a
   * different body is refused (`idempotency_key_reused`), and a new request
   * would be the message twice, which is what the request exists to stop (F2).
   */
  partial: boolean
}

/** One transcript entry, as much of it as settling reads. */
export interface SeenTurn {
  role: string
  text: string
  imageCount?: number
  /** Unix seconds. */
  at?: number
}

/**
 * A request id for a new card. `crypto.randomUUID` exists only in a secure
 * context, and the console on a home network is served over plain http, so
 * the random bytes are asked for directly when it is missing.
 */
export function newRequestID(): string {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === "function") {
    try {
      return c.randomUUID()
    } catch {
      /* below */
    }
  }
  const bytes = new Uint8Array(16)
  c.getRandomValues(bytes)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

/**
 * A mark for this page's cards. The token counts from one on every load, so two
 * tabs of the same browser minting their nth card in the same millisecond would
 * mint the same token — and the tokens are what tells one tab's kept cards from
 * another's in the one store they share (`persist.ts`).
 */
const pageMark = newRequestID().slice(0, 8)

/** `OPTIMISTIC_LIFETIME_SECONDS`: a card the transcript never confirms goes after this. */
export const PENDING_LIFETIME_MS = 10 * 60 * 1000

/**
 * How far before the send a turn may be dated and still be it. The Swift app
 * allows ten seconds against the Mac's own clock; this page has only the
 * browser's, which on a phone can be ahead of the Mac's.
 */
const EARLY_MS = 2 * 60 * 1000

/**
 * The words as the transcript would carry them. The daemon takes Claude's
 * `[Image #n]` markers out of a turn that carries pictures and counts them
 * (`canonicalImageContent`), and a picture-only turn is the markers alone.
 * A `!` command is the same line with or without a space after the `!`: the
 * record keeps whichever arrived, and `! ls` and `!ls` run the same command.
 */
export function turnWords(text: string, imageCount: number): string {
  const words = imageCount > 0 ? text.replace(/\[Image #\d+\]/g, "") : text
  const flat = words.replace(/\s+/g, " ").trim().replace(/^! /, "!")
  return flat + (imageCount > 0 ? "\u0001pictures" : "")
}

/** One turn's identity: when it was written and what it says. */
function occurrenceKey(turn: SeenTurn): string {
  return String(turn.at || 0) + "\u0001" + turnWords(turn.text, turn.imageCount || 0)
}

function occurrences(turns: readonly SeenTurn[]): Map<string, number> {
  const found = new Map<string, number>()
  for (const turn of turns) {
    if (turn.role !== "user") continue
    const key = occurrenceKey(turn)
    found.set(key, (found.get(key) ?? 0) + 1)
  }
  return found
}

function inWindow(turn: SeenTurn, card: PendingSend): boolean {
  // A turn with no time is judged by its words and `known` alone.
  if (!turn.at) return true
  const ms = turn.at * 1000
  return ms >= card.sentAt - EARLY_MS && ms <= card.sentAt + PENDING_LIFETIME_MS
}

export class PendingSends {
  private cards: PendingSend[] = []
  private seen = new Map<string, readonly SeenTurn[]>()
  private version = 0
  private count = 0
  private listeners = new Set<() => void>()

  /** For `useSyncExternalStore`. */
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }
  getVersion = (): number => this.version

  /** The cards for one session, oldest first. */
  of(session: string): PendingSend[] {
    return this.cards.filter((card) => card.session === session)
  }

  /** A card for words that are about to be sent. */
  add(session: string, text: string, pictures: readonly string[], now: number): PendingSend {
    this.count += 1
    const card: PendingSend = {
      token: `p${this.count}-${now}-${pageMark}`,
      session,
      text,
      pictures: [...pictures],
      request: newRequestID(),
      state: "sending",
      failure: "",
      absent: false,
      sentAt: now,
      acceptedAt: 0,
      known: occurrences(this.seen.get(session) ?? []),
      checking: false,
      partial: false,
    }
    this.cards.push(card)
    this.changed()
    return card
  }

  /** Every card on the page, oldest first: what `persist.ts` keeps between page loads. */
  all(): readonly PendingSend[] {
    return this.cards
  }

  /**
   * Cards kept by an earlier load of this page, put back (F4). A token already
   * on the page is left alone: this page's own card is the live one.
   */
  restore(cards: readonly PendingSend[]): void {
    let added = 0
    for (const card of cards) {
      if (this.find(card.token)) continue
      this.cards.push(card)
      added += 1
    }
    if (!added) return
    // Past the restored tokens, so a card made later cannot be given one of them.
    this.count += added
    this.cards.sort((a, b) => a.sentAt - b.sentAt)
    this.changed()
  }

  /** The daemon took it: the bytes reached the terminal. */
  accepted(token: string, now: number): void {
    const card = this.find(token)
    if (!card) return
    card.state = "accepted"
    card.acceptedAt = now
    card.failure = ""
    this.changed()
  }

  /** It did not go: the refusal proves nothing was typed. */
  failed(token: string, code: string): void {
    const card = this.find(token)
    if (!card) return
    card.state = "failed"
    card.failure = code
    card.checking = false
    card.absent = false
    this.changed()
  }

  /**
   * It may have gone. Nothing this end heard says whether the words reached
   * the terminal, so the card says that, and is looked at rather than sent
   * again (F3). `absent` is what the last fresh read of the transcript said.
   */
  uncertain(token: string, code: string, absent = false): void {
    const card = this.find(token)
    if (!card) return
    card.state = "unknown"
    card.failure = code
    card.checking = false
    card.absent = absent
    this.changed()
  }

  /** "Look": the card says it is reading the transcript. */
  looking(token: string): PendingSend | null {
    const card = this.find(token)
    if (!card || card.state !== "unknown" || card.checking) return null
    card.checking = true
    this.changed()
    return card
  }

  /** One card, if it is still on the page. */
  card(token: string): PendingSend | undefined {
    return this.find(token)
  }

  /**
   * "Try again", first half: the card says it is sending while the page reads
   * the transcript once more. A failure at this end does not mean the words did
   * not arrive — across Clawdline Cloud a Mac can run a command whose answer
   * never comes back — and the read is what can tell. `known` is left as the
   * failed attempt had it, so a turn that attempt produced still settles the
   * card instead of being typed a second time.
   */
  retrying(token: string): PendingSend | null {
    const card = this.find(token)
    if (!card || card.partial) return null
    if (!(card.state === "failed" || (card.state === "unknown" && card.absent))) return null
    card.state = "sending"
    card.failure = ""
    card.checking = true
    this.changed()
    return card
  }

  /**
   * The same words again, as a new attempt. What the transcript holds now is
   * what the new attempt must not be mistaken for — including the first
   * attempt, if that one arrived after all.
   */
  resend(token: string, now: number): PendingSend | null {
    const card = this.find(token)
    if (!card || card.partial) return null
    if (card.state !== "failed" && !card.checking) return null
    card.state = "sending"
    card.failure = ""
    card.checking = false
    card.absent = false
    card.sentAt = now
    card.acceptedAt = 0
    card.known = occurrences(this.seen.get(card.session) ?? [])
    this.changed()
    return card
  }

  /** Put away a card the reader has finished with. */
  dismiss(token: string): void {
    const before = this.cards.length
    this.cards = this.cards.filter((card) => card.token !== token)
    if (this.cards.length !== before) this.changed()
  }

  /**
   * A fresh read of one session's transcript: the cards it confirms go, and
   * so does an accepted card older than its lifetime.
   *
   * Cards still on their way are matched first, in the order they were sent,
   * and failed ones after them, so that words sent again after a failure are
   * credited to the attempt that went.
   */
  reconcile(session: string, turns: readonly SeenTurn[], now: number): void {
    this.seen.set(session, turns)
    let changed = false
    const settled = new Set<PendingSend>()
    for (const card of this.of(session)) {
      if (card.state === "accepted" && now - card.acceptedAt > PENDING_LIFETIME_MS) {
        settled.add(card)
        changed = true
      }
    }
    const mine = this.of(session).filter((card) => !settled.has(card))
    const stopped = (card: PendingSend) => card.state === "failed" || card.state === "unknown"
    const order = [...mine.filter((card) => !stopped(card)), ...mine.filter(stopped)]
    const used = new Set<number>()
    for (const card of order) {
      const want = turnWords(card.text, card.pictures.length)
      const seenSoFar = new Map<string, number>()
      let found = -1
      let foundKey = ""
      let foundCount = 0
      for (let i = 0; i < turns.length; i++) {
        const turn = turns[i]
        if (turn.role !== "user") continue
        const key = occurrenceKey(turn)
        const n = (seenSoFar.get(key) ?? 0) + 1
        seenSoFar.set(key, n)
        if (used.has(i)) continue
        if (turnWords(turn.text, turn.imageCount || 0) !== want) continue
        if (!inWindow(turn, card)) continue
        if ((card.known.get(key) ?? 0) >= n) continue
        found = i
        foundKey = key
        foundCount = n
        break
      }
      if (found < 0) continue
      used.add(found)
      settled.add(card)
      changed = true
      // The turn that settled this card stays in the transcript. A later card
      // with the same words, read again after this one has gone, must not
      // take it too.
      for (const other of mine) {
        if (settled.has(other)) continue
        other.known.set(foundKey, Math.max(other.known.get(foundKey) ?? 0, foundCount))
      }
    }
    if (!changed) return
    this.cards = this.cards.filter((card) => !settled.has(card))
    this.changed()
  }

  private find(token: string): PendingSend | undefined {
    return this.cards.find((card) => card.token === token)
  }

  private changed(): void {
    this.version += 1
    for (const listener of [...this.listeners]) listener()
  }
}
