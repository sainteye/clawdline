/*
 * Sending a message, with its card.
 *
 * The composer sends through here and so does a card's "try again" and
 * "look", so every path reports the same way: the card (`pending.ts`) says how
 * it went, and the transcript draws the card. What each of those decides is
 * `sender.ts`; this file is the part that talks to the daemon.
 *
 * The daemon may be this machine or one across Clawdline Cloud
 * (`cloud/relay-writer.ts`); nothing here knows which. Both are asked the same
 * way: `POST /v1/sessions/<id>/send` under the card's one Idempotency-Key, so a
 * second attempt of the same card is answered with the first attempt's answer
 * rather than typed again (F2).
 */
import { client } from "../client.js"
import { outcomeOf } from "./outcome.js"
import { PendingSends, type PendingSend } from "./pending.js"
import { postCard, readTranscript, Sender } from "./sender.js"

/** The page's one set of cards: a card outlives the composer that made it and the session being switched away from. */
export const pendingSends = new PendingSends()

const url = (path: string) => client.url(path)
// `fetch` is looked up per request, as `client` does: across Clawdline Cloud it
// is the relay seam's (`cloud/install.ts`).
const doFetch: typeof fetch = (input, init) => globalThis.fetch(input, init)

const sender = new Sender({
  cards: pendingSends,
  now: () => Date.now(),
  post: (card) => postCard(doFetch, url, card),
  readBack: (session) => readTranscript(doFetch, url, session),
  outcomeOf,
})

/**
 * Post one card's words. Resolves with the refusal's code, or "" when the
 * daemon took them; the card has been told either way.
 */
export function deliver(card: PendingSend): Promise<string> {
  return sender.deliver(card)
}

/**
 * `deliver`, or sooner: the moment the card has gone because its turn was read
 * back. What the answer could still say — "the Mac has it" — the transcript
 * already shows, and across Clawdline Cloud an answer that was lost on the way
 * back is only given up on after a minute (`cloud-client.js`
 * `READ_TIMEOUT_MS`); the composer should not hold its button for that.
 */
export function deliverUntilSeen(card: PendingSend): Promise<string> {
  return new Promise((resolve) => {
    const stop = pendingSends.subscribe(() => {
      if (pendingSends.card(card.token)) return
      stop()
      resolve("")
    })
    void deliver(card).then((code) => {
      stop()
      resolve(code)
    })
  })
}

/** A card's "try again" (`Sender.resend`): read first, and send only on a read that shows no turn. */
export function resend(token: string): Promise<void> {
  return sender.resend(token)
}

/** An unknown card's "look" (`Sender.look`): read the transcript; never send. */
export function look(token: string): Promise<void> {
  return sender.look(token)
}
