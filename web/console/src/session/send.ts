/*
 * Sending a message, with its card.
 *
 * The composer sends through here and so does a failed card's "try again", so
 * both paths report the same way: the card (`pending.ts`) says how it went,
 * and the transcript draws the card.
 */
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import { sendWithPictures } from "../legacy/shots-bridge.js"
import { PendingSends, type PendingSend } from "./pending.js"

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

/** A failed card's "try again": the same words and pictures, as a new attempt. */
export function resend(token: string): void {
  const card = pendingSends.resend(token, Date.now())
  if (card) void deliver(card)
}
