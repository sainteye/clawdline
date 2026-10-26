import type { PendingSend, PendingSends } from "./pending.js"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { KEPT_CARDS } from "./persist.ts"

/** Explicitly queued inputs stay in this browser's existing persisted cards. */
export class NextMessages {
  private last: { session: string; observation: string } | null = null
  private cards: PendingSends
  constructor(cards: PendingSends) { this.cards = cards }

  add(session: string, text: string, pictures: readonly string[], now: number): PendingSend | null {
    // Share the existing card budget; never evict a queued input to add another.
    if (this.cards.all().length >= KEPT_CARDS) return null
    return this.cards.add(session, text, pictures, now, true)
  }

  take(session: string, idle: boolean, observation: string, write: boolean, now: number): PendingSend | null {
    if (!write || !idle || !observation || this.last?.session === session && this.last.observation === observation) return null
    const mine = this.cards.of(session)
    if (mine.some(card => card.state === "sending")) return null
    const card = mine.find(card => card.state === "queued")
    if (!card) return null
    const claimed = this.cards.startQueued(card.token, now)
    if (claimed) this.last = { session, observation }
    return claimed
  }
}
