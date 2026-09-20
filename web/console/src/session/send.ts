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
 *
 * This file is also where the cards are given somewhere to live between page
 * loads (`persist.ts`, F4): this browser's `localStorage`, read once here and
 * written after every change. None of it is asked of the daemon, and nothing on
 * the way back is sent.
 */
import { client } from "../client.js"
import { outcomeOf } from "./outcome.js"
import { PendingSends, turnWords, type PendingSend } from "./pending.js"
import { Cards, type CardStore } from "./persist.js"
import { postCard, readTranscript, Sender } from "./sender.js"

/** The page's one set of cards: a card outlives the composer that made it and the session being switched away from. */
export const pendingSends = new PendingSends()

/** Beside the other per-browser things this origin keeps (`bar/history.ts`, `legacy/order-bridge.ts`). */
const CARDS_KEY = "clawdline.session.cards"

// Every call is the browser's, and every one of them can throw — a private
// window, site data this browser will not give out, or none at all. `Cards`
// catches; this only has to find it.
const browserStore: CardStore | null = (() => {
  try {
    return globalThis.localStorage ? { read: () => localStorage.getItem(CARDS_KEY), write: (value) => localStorage.setItem(CARDS_KEY, value) } : null
  } catch {
    return null
  }
})()

const kept = new Cards({ store: browserStore, turnWords })
pendingSends.subscribe(() => kept.keep(pendingSends.all(), Date.now()))

/**
 * Which daemon this page ended up talking to, and with it the cards kept for
 * that one: they are put back on the page now, and what the page holds from
 * here on is kept under that name.
 *
 * The daemon's own console is one daemon per origin and says so at once. The
 * hosted console is every machine on the account under one origin and cannot,
 * because which machine is a person's press (`cloud/CloudGate.tsx` `choose`) —
 * and until it is pressed there is no session open and so no card to keep. A
 * session id is a terminal id, and a tmux pane is `%1` on every Mac that has
 * one, so a card put back under the wrong machine would be words addressed to
 * whatever that machine's `%1` holds.
 */
export function cardsAreFor(daemon: string): void {
  pendingSends.restore(kept.restore(daemon, Date.now()))
}

// The build's own declaration, as `main.tsx` reads it — never a guess from the
// hostname, since the daemon's page is served through tunnels too.
if (!import.meta.env.VITE_HOSTED_CONSOLE) cardsAreFor("")

// Another tab of this origin writing the store: the cards it has finished with
// — dismissed there, or settled there by a turn in the transcript — go here
// too, so that "no thanks" means gone rather than gone until this tab writes
// its copy back. Nothing else of that tab's is adopted; each tab keeps its own
// cards, and a card in both is one card, under one request.
try {
  globalThis.addEventListener?.("storage", (event) => {
    const changed = event as StorageEvent
    if (changed.key !== null && changed.key !== CARDS_KEY) return
    for (const token of kept.letGo(changed.newValue ?? browserStore?.read() ?? null, Date.now())) pendingSends.dismiss(token)
  })
} catch {
  /* no window to listen on; one tab is the whole of it */
}

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
 * back. What the answer could still say — "the machine has it" — the transcript
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
