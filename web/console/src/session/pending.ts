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
 *   "sending", then the catalog's "the Mac has it".
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
 * Nothing here is imported at run time, so `node --test` loads it as it is;
 * the part that talks to the daemon is `send.ts`.
 */

export type PendingState = "sending" | "accepted" | "failed"

export interface PendingSend {
  readonly token: string
  readonly session: string
  readonly text: string
  /** The pictures as `data:` URLs, kept so that sending again sends them too. */
  readonly pictures: readonly string[]
  state: PendingState
  /** The refusal's code, while `failed`. */
  failure: string
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
}

/** One transcript entry, as much of it as settling reads. */
export interface SeenTurn {
  role: string
  text: string
  imageCount?: number
  /** Unix seconds. */
  at?: number
}

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
 */
export function turnWords(text: string, imageCount: number): string {
  const words = imageCount > 0 ? text.replace(/\[Image #\d+\]/g, "") : text
  return words.replace(/\s+/g, " ").trim() + (imageCount > 0 ? "\u0001pictures" : "")
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
      token: `p${this.count}-${now}`,
      session,
      text,
      pictures: [...pictures],
      state: "sending",
      failure: "",
      sentAt: now,
      acceptedAt: 0,
      known: occurrences(this.seen.get(session) ?? []),
      checking: false,
    }
    this.cards.push(card)
    this.changed()
    return card
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

  /** It did not go, or this end could not tell that it did. */
  failed(token: string, code: string): void {
    const card = this.find(token)
    if (!card) return
    card.state = "failed"
    card.failure = code
    this.changed()
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
    if (!card || card.state !== "failed") return null
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
    if (!card || (card.state !== "failed" && !card.checking)) return null
    card.state = "sending"
    card.failure = ""
    card.checking = false
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
    const order = [...mine.filter((card) => card.state !== "failed"), ...mine.filter((card) => card.state === "failed")]
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
