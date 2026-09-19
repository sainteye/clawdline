/*
 * Sending a message, with its card.
 *
 * The composer sends through here and so does a failed card's "try again", so
 * both paths report the same way: the card (`pending.ts`) says how it went,
 * and the transcript draws the card.
 *
 * The daemon may be this machine or one across Clawdline Cloud
 * (`cloud/relay-writer.ts`); nothing here knows which. The one thing the far
 * case changes is written into "try again" for both: an attempt that failed at
 * this end may have been typed at the other, so the transcript is read before
 * the words go a second time.
 */
import type { TranscriptPage } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import { sendWithPictures } from "../legacy/shots-bridge.js"
import { PendingSends, type PendingSend, type SeenTurn } from "./pending.js"

/** The page's one set of cards: a card outlives the composer that made it and the session being switched away from. */
export const pendingSends = new PendingSends()

/**
 * Post one card's words. Resolves with the refusal's code, or "" when the
 * daemon took them; the card has been told either way.
 */
export async function deliver(card: PendingSend): Promise<string> {
  try {
    await (card.pictures.length
      ? sendWithPictures(card.session, card.text, [...card.pictures])
      : client.send(card.session, card.text))
    pendingSends.accepted(card.token, Date.now())
    return ""
  } catch (err) {
    const code = err instanceof RefusalError ? err.code : "unexpected_error"
    pendingSends.failed(card.token, code)
    return code
  }
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

/**
 * A failed card's "try again": the same words and pictures, as a new attempt —
 * once the transcript, read fresh, shows the failed attempt did not arrive.
 * If it did, reading it settles the card and nothing is sent.
 */
export async function resend(token: string): Promise<void> {
  const card = pendingSends.retrying(token)
  if (!card) return
  const turns = await readBack(card.session)
  if (turns) pendingSends.reconcile(card.session, turns, Date.now())
  const again = pendingSends.resend(token, Date.now())
  if (again) await deliver(again)
}

/**
 * The session's transcript as the daemon has it now, or null when it could
 * not be read — in which case the words go again, as they did before this
 * check existed. `no-store` asks past any answer held from before: the Cloud
 * seam reuses transcripts, and one from before the failure cannot tell.
 */
async function readBack(session: string): Promise<SeenTurn[] | null> {
  try {
    const res = await fetch(client.url(`/v1/transcript?session=${encodeURIComponent(session)}&limit=200`), {
      cache: "no-store",
      credentials: "same-origin",
    })
    if (!res.ok) return null
    const page = (await res.json()) as TranscriptPage
    return Array.isArray(page.entries) ? (page.entries as SeenTurn[]) : null
  } catch {
    return null
  }
}
